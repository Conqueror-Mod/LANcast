sub init()
    m.halo = m.top.findNode("halo")
    m.ring = m.top.findNode("ring")
    m.fill = m.top.findNode("fill")
    m.title = m.top.findNode("title")
    m.sub = m.top.findNode("sub")
end sub

sub layout()
    w = m.top.width
    h = m.top.height
    if w <= 0 or h <= 0 then return
    m.halo.width = w + 16 : m.halo.height = h + 16 : m.halo.translation = [-8, -8]
    m.ring.width = w : m.ring.height = h
    m.fill.width = w - 6 : m.fill.height = h - 6 : m.fill.translation = [3, 3]
    m.title.width = w - 56
    m.sub.width = w - 56
end sub

sub show()
    c = m.top.itemContent
    if c = invalid then return
    m.title.text = c.title
    m.sub.text = c.description
end sub

' Gold follows focus only while the grid itself has it (MarkupGrid sets
' gridHasFocus), so moving to a button below never leaves a second gold edge.
sub onFocus()
    p = m.top.focusPercent
    if not m.top.gridHasFocus then p = 0
    m.ring.opacity = 0.2 + 0.8 * p
    m.halo.opacity = 0.13 * p
end sub
