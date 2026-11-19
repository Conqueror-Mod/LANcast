' One HTTPS request from a Task thread (roUrlTransfer cannot run on the render
' thread).
'
' conn says how the server is trusted:
'   { public: true }         a public name, checked against Roku's own CAs
'   { pemPath: "tmp:/…" }    a LAN server, pinned to the certificate paired with
'   { unverified: true }     only to read the certificate before pairing
' and carries key when signed in.
function HttpRequest(method as string, url as string, body as string, conn as object) as object
    t = CreateObject("roUrlTransfer")
    port = CreateObject("roMessagePort")
    t.SetMessagePort(port)
    t.SetUrl(url)
    t.RetainBodyOnError(true)
    t.EnableEncodings(true)
    if conn.unverified = true then
        t.SetCertificatesFile("common:/certs/ca-bundle.crt")
        t.EnablePeerVerification(false)
        t.EnableHostVerification(false)
    else if conn.public = true then
        t.SetCertificatesFile("common:/certs/ca-bundle.crt")
    else
        t.SetCertificatesFile(conn.pemPath)
    end if
    t.InitClientCertificates()
    t.AddHeader("Accept", "application/json")
    if conn.key <> invalid and conn.key <> "" then t.AddHeader("Authorization", "Bearer " + conn.key)

    if method = "GET" then
        started = t.AsyncGetToString()
    else
        t.SetRequest(method)
        t.AddHeader("Content-Type", "application/json")
        started = t.AsyncPostFromString(body)
    end if
    if not started then return { code: 0, body: "", error: "could not start the request" }

    msg = wait(15000, port)
    if type(msg) <> "roUrlEvent" then
        t.AsyncCancel()
        return { code: 0, body: "", error: "no answer within 15 seconds" }
    end if
    return { code: msg.GetResponseCode(), body: msg.GetString(), error: msg.GetFailureReason() }
end function

' ErrorText is what a failed request should say on screen.
function ErrorText(r as object) as string
    if r.code > 0 then
        ' Only an object can carry an error: a successful answer may be an array,
        ' and reading .error off an array stops the app.
        j = ParseJson(r.body)
        if type(j) = "roAssociativeArray" and type(j.error) = "roAssociativeArray" and j.error.message <> invalid then return j.error.message
        return "the server answered " + r.code.toStr()
    end if
    if r.error <> invalid and r.error <> "" then return r.error
    return "no answer"
end function
