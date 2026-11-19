sub init()
    m.top.functionName = "work"
end sub

sub work()
    r = HttpRequest(m.top.method, m.top.url, m.top.body, { public: m.top.public, pemPath: m.top.pemPath, key: m.top.key })
    data = invalid
    detail = ""
    if r.code >= 200 and r.code < 300 then
        if r.body <> "" then data = ParseJson(r.body)
    else
        detail = ErrorText(r)
    end if
    m.top.response = { code: r.code, data: data, error: detail }
end sub
