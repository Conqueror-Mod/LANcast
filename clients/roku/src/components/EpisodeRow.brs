sub init()
    m.halo = m.top.findNode("halo")
    m.ring = m.top.findNode("ring")
    m.thumb = m.top.findNode("thumb")
    m.title = m.top.findNode("title")
    m.sub = m.top.findNode("sub")
end sub

sub show()
    c = m.top.itemContent
    if c = invalid then return
    m.title.text = c.title
    m.sub.text = c.description
    m.thumb.uri = c.HDPosterUrl
end sub

sub paint()
    p = m.top.focusPercent
    if not m.top.listHasFocus then p = 0
    m.ring.opacity = 0.2 + 0.8 * p
    m.halo.opacity = 0.13 * p
    if p > 0.5 then m.title.color = "0xF2F5FBFF" else m.title.color = "0xC3CBE2FF"
end sub
