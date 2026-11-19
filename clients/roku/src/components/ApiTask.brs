sub init()
    m.top.functionName = "work"
end sub

sub work()
    r = HttpRequest("GET", m.top.url, "", { public: m.top.public, pemPath: m.top.pemPath, key: m.top.key })
    data = invalid
    if r.code = 200 then data = ParseJson(r.body)
    m.top.response = { code: r.code, data: data, error: ErrorText(r) }
end sub
