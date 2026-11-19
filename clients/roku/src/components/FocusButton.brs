sub init()
    m.halo = m.top.findNode("halo")
    m.ring = m.top.findNode("ring")
    m.fill = m.top.findNode("fill")
    m.label = m.top.findNode("label")
    m.top.observeField("focusedChild", "onFocus")
    layout()
end sub

sub layout()
    w = m.top.width
    h = m.top.height
    m.halo.width = w + 16 : m.halo.height = h + 16 : m.halo.translation = [-8, -8]
    m.ring.width = w : m.ring.height = h
    m.fill.width = w - 6 : m.fill.height = h - 6 : m.fill.translation = [3, 3]
    m.label.width = w : m.label.height = h
    m.label.text = m.top.text
end sub

sub onFocus()
    focused = m.top.hasFocus()
    if focused then
        m.ring.opacity = 1.0
        m.halo.opacity = 0.13
        m.label.color = "0xF2F5FBFF"
    else
        m.ring.opacity = 0.2
        m.halo.opacity = 0
        m.label.color = "0xC3CBE2FF"
    end if
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if press and key = "OK" then
        m.top.selected = true
        return true
    end if
    return false
end function
