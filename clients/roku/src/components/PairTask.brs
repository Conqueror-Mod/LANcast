' ADR 0125 §1: POST /api/devices/pair, show the code, poll no faster than the
' server asks. The pairing request and every poll are pinned to the
' certificate the code is computed from (amendment 1), so a relay in the
' middle cannot read the key out of the poll.

sub init()
    m.top.functionName = "work"
end sub

sub work()
    conn = { public: m.top.public }
    if not m.top.public then
        conn.pemPath = "tmp:/pair.pem"
        WriteAsciiFile(conn.pemPath, m.top.pem)
    end if

    body = FormatJson({ name: m.top.name, device: "Roku" })
    r = HttpRequest("POST", m.top.base + "/api/devices/pair", body, conn)
    start = invalid
    if r.code = 200 then start = ParseJson(r.body)
    if start = invalid or start.pairing = invalid or start.nonce = invalid then
        finish("error", "Could not start pairing: " + ErrorText(r))
        return
    end if

    code = PairingCode(start.nonce, m.top.pem)
    if code = "" then
        finish("error", "Could not work out the pairing code from the server's certificate.")
        return
    end if
    m.top.code = code
    m.top.outcome = "waiting"

    interval = 5
    if start.interval <> invalid and start.interval > interval then interval = start.interval
    expires = 600
    if start.expires_in <> invalid then expires = start.expires_in
    deadline = CreateObject("roDateTime").AsSeconds() + expires
    pollBody = FormatJson({ pairing: start.pairing })

    while CreateObject("roDateTime").AsSeconds() < deadline
        ' Sleep in short steps so Back is answered at once.
        for i = 1 to interval * 4
            if m.top.cancel then return
            sleep(250)
        end for
        p = HttpRequest("POST", m.top.base + "/api/devices/pair/poll", pollBody, conn)
        if p.code = 202 then
            ' Nobody has answered yet.
        else if p.code = 200 then
            j = ParseJson(p.body)
            if j = invalid or j.key = invalid then
                finish("error", "The server's answer could not be read.")
                return
            end if
            userName = ""
            if j.user <> invalid and j.user.name <> invalid then userName = j.user.name
            keyId = ""
            if j.key_id <> invalid then keyId = j.key_id
            m.top.result = { key: j.key, keyId: keyId, userName: userName }
            finish("approved", "")
            return
        else if p.code = 410 then
            finish("refused", "This code was refused or has expired.")
            return
        else if p.code = 429 then
            interval = interval + 1
        else
            m.top.message = "Still waiting (" + ErrorText(p) + ")."
        end if
    end while
    finish("refused", "This code has expired.")
end sub

sub finish(outcome as string, message as string)
    m.top.message = message
    m.top.outcome = outcome
end sub
