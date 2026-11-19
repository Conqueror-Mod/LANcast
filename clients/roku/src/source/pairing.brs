' The pairing code (ADR 0125 §2): computed here from what this TV was served,
' never issued. It must match internal/devicepair.Code exactly:
'
'   base32(SHA-256("LANcast pairing v1" || 0x00 || nonce || certDER))[:8]
'
' in Crockford's alphabet, most significant bits first. A machine in the
' middle serves its own certificate, so this TV shows a code the server never
' filed and the approval fails as an unknown code.

function PairingAlphabet() as string
    return "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
end function

' PairingDigest is the SHA-256, as lowercase hex, of the domain, a zero byte,
' the nonce and the certificate.
function PairingDigest(nonce as object, certDer as object) as string
    msg = CreateObject("roByteArray")
    msg.FromAsciiString("LANcast pairing v1")
    msg.Push(0)
    msg.Append(nonce)
    msg.Append(certDer)
    d = CreateObject("roEVPDigest")
    d.Setup("sha256")
    return LCase(d.Process(msg))
end function

' PairingCodeFromDigest is the first 40 bits of a digest as eight symbols.
function PairingCodeFromDigest(hex as string) as string
    ba = CreateObject("roByteArray")
    ba.FromHexString(hex)
    alphabet = PairingAlphabet()
    code = ""
    buf = 0
    bits = 0
    for i = 0 to 4
        buf = (buf << 8) or ba[i]
        bits = bits + 8
        while bits >= 5
            bits = bits - 5
            code = code + Mid(alphabet, ((buf >> bits) and 31) + 1, 1)
            buf = buf and ((1 << bits) - 1)
        end while
    end for
    return code
end function

' PairingCode is the code for a nonce (base64, as the server sends it) and the
' certificate this TV read (PEM).
function PairingCode(nonceB64 as string, pem as string) as string
    nonce = CreateObject("roByteArray")
    nonce.FromBase64String(nonceB64)
    der = PemToDer(pem)
    if nonce.Count() = 0 or der.Count() = 0 then return ""
    return PairingCodeFromDigest(PairingDigest(nonce, der))
end function

' FormatCode shows a code as two groups of four, the way the web screen reads it.
function FormatCode(code as string) as string
    if Len(code) <> 8 then return code
    return Left(code, 4) + "-" + Right(code, 4)
end function

' PemToDer decodes the first certificate in a PEM: the leaf.
function PemToDer(pem as string) as object
    body = ""
    inside = false
    for each line in pem.Split(Chr(10))
        l = line.Trim()
        if Left(l, 11) = "-----BEGIN " then
            inside = true
        else if Left(l, 9) = "-----END " then
            if inside then exit for
        else if inside then
            body = body + l
        end if
    end for
    der = CreateObject("roByteArray")
    if body <> "" then der.FromBase64String(body)
    return der
end function

' PairingSelfCheck runs the server's own test vector through this device's
' digest and alphabet (internal/devicepair: nonce of sixteen 0x01 bytes, the
' certificate "a certificate"). The ADR left open whether BrightScript's
' SHA-256 works on the TV; this answers it every time the app starts.
function PairingSelfCheck() as object
    nonce = CreateObject("roByteArray")
    for i = 1 to 16
        nonce.Push(1)
    end for
    cert = CreateObject("roByteArray")
    cert.FromAsciiString("a certificate")
    hex = PairingDigest(nonce, cert)
    code = PairingCodeFromDigest(hex)
    ok = hex = "0bfe233a19374d57060fc61c36799a99ec0d85dedf68a442e228bf484af2d22a" and code = "1FZ26EGS"
    return { ok: ok, code: code, digest: hex }
end function
