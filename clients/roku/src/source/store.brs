' What this TV remembers, in the registry: the server, how it is trusted, and
' the key it was given when it was paired. The key is the approver's API key
' (ADR 0061); revoking it in LANcast's Settings signs this TV out.

function StoreRead() as object
    sec = CreateObject("roRegistrySection", "LANcast")
    out = {}
    for each k in ["server", "public", "pem", "key", "keyId", "userName"]
        if sec.Exists(k) then out[k] = sec.Read(k) else out[k] = ""
    end for
    return out
end function

sub StoreWrite(values as object)
    sec = CreateObject("roRegistrySection", "LANcast")
    for each k in values
        sec.Write(k, values[k])
    end for
    sec.Flush()
end sub

' StoreForgetKey signs this TV out and keeps the server.
sub StoreForgetKey()
    sec = CreateObject("roRegistrySection", "LANcast")
    for each k in ["key", "keyId", "userName"]
        sec.Delete(k)
    end for
    sec.Flush()
end sub

sub StoreForgetAll()
    sec = CreateObject("roRegistrySection", "LANcast")
    for each k in sec.GetKeyList()
        sec.Delete(k)
    end for
    sec.Flush()
end sub

' PinPath writes a pinned certificate where roUrlTransfer and the video player
' can read it, and says where.
function PinPath(pem as string) as string
    path = "tmp:/lancast.pem"
    WriteAsciiFile(path, pem)
    return path
end function
