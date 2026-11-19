sub init()
    m.code = m.top.findNode("code")
    m.status = m.top.findNode("status")
    m.where = m.top.findNode("where")
    m.again = m.top.findNode("again")
    m.change = m.top.findNode("change")
    m.again.observeField("selected", "onAgain")
    m.change.observeField("selected", "onChange")
    m.top.observeField("focusedChild", "onFocus")
end sub

sub onFocus()
    if m.top.hasFocus() then m.again.setFocus(true)
end sub

sub showWhere()
    m.where.text = "On a phone or computer signed in to LANcast, open Settings › Security › Pair a device, or go to " + m.top.server + "/pair, and type this code:"
end sub

sub showCode()
    if m.top.code = "" then
        m.code.text = "…"
    else
        m.code.text = FormatCode(m.top.code)
    end if
end sub

sub showMessage()
    m.status.text = m.top.message
end sub

sub onAgain()
    m.top.newCode = true
end sub

sub onChange()
    m.top.changeServer = true
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "right" and m.again.hasFocus() then
        m.change.setFocus(true)
        return true
    else if key = "left" and m.change.hasFocus() then
        m.again.setFocus(true)
        return true
    end if
    return false
end function
