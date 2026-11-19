' Signed-in requests from any screen. Where the server is, how it is trusted
' and the TV's key live on m.global, set once at sign-in by MainScene, so a
' screen only says what it wants.

' ApiGet starts one GET and calls handler (a sub taking the field event) with
' its response. Keep the returned task: a task nothing holds is collected.
function ApiGet(path as string, handler as string) as object
    g = m.global
    t = CreateObject("roSGNode", "ApiTask")
    t.url = g.lcBase + path
    t.public = g.lcPublic
    t.pemPath = g.lcPemPath
    t.key = g.lcKey
    t.observeField("response", handler)
    t.control = "run"
    return t
end function

' ApiAnswer unwraps a response, and tells MainScene when the key was revoked.
function ApiAnswer(e as object) as object
    r = e.getData()
    if r.code = 401 then m.global.lcSignedOut = true
    return r
end function

' ArtworkUrl is where a picture is, or "" when there is none. Posters fetch it
' through the scene's shared HTTP agent, which carries the pin and the key.
function ArtworkUrl(hash as dynamic, size as string) as string
    if hash = invalid or hash = "" then return ""
    return m.global.lcBase + "/api/artwork/" + hash + "?size=" + size
end function
