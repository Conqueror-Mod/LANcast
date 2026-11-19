sub init()
    m.halo = m.top.findNode("halo")
    m.ring = m.top.findNode("ring")
    m.fill = m.top.findNode("fill")
    m.label = m.top.findNode("label")
    m.bold = CreateObject("roSGNode", "Font")
    m.bold.uri = "font:BoldSystemFontFile"
    m.bold.size = 30
    m.plain = m.label.font
end sub

sub layout()
    w = m.top.width
    h = m.top.height
    if w <= 0 or h <= 0 then return
    m.halo.width = w + 16 : m.halo.height = h + 16
    m.ring.width = w : m.ring.height = h
    m.fill.width = w - 6 : m.fill.height = h - 6
    m.label.width = w - 48 : m.label.height = h
end sub

sub show()
    c = m.top.itemContent
    if c = invalid then return
    m.label.text = c.title
    c.observeFieldScoped("shown", "paint")
    paint()
end sub

sub paint()
    p = m.top.focusPercent
    if not m.top.listHasFocus then p = 0
    m.ring.opacity = 0.2 + 0.8 * p
    m.halo.opacity = 0.13 * p
    c = m.top.itemContent
    if c <> invalid and c.shown = true then
        m.label.font = m.bold
        m.label.color = "0xF2F5FBFF"
    else
        m.label.font = m.plain
        m.label.color = "0xC3CBE2FF"
    end if
end sub
