sub init()
    m.halo = m.top.findNode("halo")
    m.ring = m.top.findNode("ring")
    m.stand = m.top.findNode("stand")
    m.poster = m.top.findNode("poster")
    m.title = m.top.findNode("title")
    m.sub = m.top.findNode("sub")
end sub

sub show()
    c = m.top.itemContent
    if c = invalid then return
    m.stand.text = c.title
    m.title.text = c.title
    m.sub.text = c.description
    m.poster.uri = c.HDPosterUrl
end sub

' Gold follows focus only while the grid itself has it.
sub onFocus()
    p = m.top.focusPercent
    if not m.top.gridHasFocus then p = 0
    m.ring.opacity = 0.2 + 0.8 * p
    m.halo.opacity = 0.13 * p
    if p > 0.5 then m.title.color = "0xF2F5FBFF" else m.title.color = "0xC3CBE2FF"
end sub
