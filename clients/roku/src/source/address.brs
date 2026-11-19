' NormalizeAddress turns what a person typed into a server's base URL.
'
' Always https: LANcast serves nothing else to the network. A port is kept
' when typed. Without one, an IP address or a bare machine name gets 8080,
' LANcast's own port, and a dotted name gets none, because that is a public
' name behind 443 (ADR 0125 §2 trusts those by Roku's own CAs).
function NormalizeAddress(typed as string) as string
    a = typed.Trim()
    i = Instr(1, a, "://")
    if i > 0 then a = Mid(a, i + 3)
    slash = Instr(1, a, "/")
    if slash > 0 then a = Left(a, slash - 1)
    if a = "" then return ""
    if Instr(1, a, ":") = 0 and (IsIPv4(a) or Instr(1, a, ".") = 0) then a = a + ":8080"
    return "https://" + LCase(a)
end function

function IsIPv4(host as string) as boolean
    parts = host.Split(".")
    if parts.Count() <> 4 then return false
    for each p in parts
        if p = "" or Len(p) > 3 then return false
        for k = 1 to Len(p)
            c = Asc(Mid(p, k, 1))
            if c < 48 or c > 57 then return false
        end for
    end for
    return true
end function
