' A public name verifies with Roku's own CAs, and is then trusted that way and
' never pinned (ADR 0125 §2). Anything else is read once without checking,
' and that certificate is what the pairing code binds and what every request
' after it is pinned to.

sub init()
    m.top.functionName = "work"
end sub

sub work()
    url = m.top.base + "/api/devices/pair/certificate"
    r = HttpRequest("GET", url, "", { public: true })
    if isPem(r) then
        m.top.result = { ok: true, public: true, pem: r.body }
        return
    end if
    r = HttpRequest("GET", url, "", { unverified: true })
    if isPem(r) then
        m.top.result = { ok: true, public: false, pem: r.body }
        return
    end if
    ' A LANcast from before TV pairing answers the route as a signed-in one
    ' (401, "sign in to continue") or not at all (404). Say what that means.
    if r.code = 401 or r.code = 404 then
        m.top.result = { ok: false, error: "The LANcast server at " + m.top.base + " is too old to pair a TV. Update LANcast and try again." }
        return
    end if
    m.top.result = { ok: false, error: "Could not reach a LANcast server at " + m.top.base + " (" + ErrorText(r) + ")." }
end sub

function isPem(r as object) as boolean
    return r.code = 200 and Instr(1, r.body, "-----BEGIN CERTIFICATE-----") > 0
end function
