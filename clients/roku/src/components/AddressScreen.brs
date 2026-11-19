sub init()
    m.enter = m.top.findNode("enter")
    m.status = m.top.findNode("status")
    m.enter.observeField("selected", "openKeyboard")
    m.top.observeField("focusedChild", "onFocus")
end sub

sub onFocus()
    if m.top.hasFocus() then m.enter.setFocus(true)
end sub

sub openKeyboard()
    if m.top.busy then return
    d = CreateObject("roSGNode", "StandardKeyboardDialog")
    d.title = "LANcast server address"
    d.message = ["For example 192.168.1.20:8080, or media.example.com"]
    d.buttons = ["Connect", "Cancel"]
    d.text = m.top.server
    d.observeField("buttonSelected", "onKeyboard")
    m.dialog = d
    m.top.getScene().dialog = d
end sub

sub onKeyboard()
    d = m.dialog
    text = d.text
    pressed = d.buttonSelected
    d.close = true
    m.dialog = invalid
    m.enter.setFocus(true)
    if pressed = 0 and text.Trim() <> "" then
        m.top.server = text.Trim()
        m.top.submitted = text.Trim()
    end if
end sub

sub showMessage()
    m.status.text = m.top.message
end sub

sub showBusy()
    if m.top.busy then m.status.text = "Connecting…"
end sub
