' Plays one film or episode, and the next one when Up Next says so.
'
' The server decides how (GET /api/items/{id}/playback?profile=roku):
'   direct            the file itself from /api/stream/{id}, seeking natively;
'   remux, transcode  HLS from /api/stream/{id}/hls/index.m3u8, in MPEG-TS.
' An HLS stream started at ?t= begins its own clock at zero there, so
' m.offset is added to the Video node's position everywhere a real position
' is needed: progress, markers, and the subtitle shift (?t= on the .vtt).

sub init()
    m.video = m.top.findNode("video")
    m.status = m.top.findNode("status")
    m.skip = m.top.findNode("skip")
    m.card = m.top.findNode("card")
    m.cardHead = m.top.findNode("cardHead")
    m.cardTitle = m.top.findNode("cardTitle")
    m.cardSub = m.top.findNode("cardSub")
    m.nextNow = m.top.findNode("nextNow")
    m.menu = m.top.findNode("menu")
    m.choices = m.top.findNode("choices")

    m.claims = DeviceClaims()
    m.saves = []
    m.item = invalid
    m.hls = false

    m.timer = CreateObject("roSGNode", "Timer")
    m.timer.repeat = true
    m.timer.duration = 1
    m.timer.observeField("fire", "onTick")

    m.video.observeField("state", "onState")
    m.skip.observeField("selected", "onSkip")
    m.nextNow.observeField("selected", "onNextNow")
    m.choices.observeField("itemSelected", "onChoice")
    m.top.observeField("focusedChild", "onFocus")
end sub

sub onFocus()
    if m.top.hasFocus() then m.video.setFocus(true)
end sub

sub onStart()
    s = m.top.start
    m.queue = []
    if type(s.queue) = "roArray" then m.queue = s.queue
    begin(s.item, s.at)
end sub

' ---- starting ----------------------------------------------------------

sub begin(item as object, at as dynamic)
    stopStream()
    m.item = item
    m.at = at
    m.lastMs = -1
    m.audio = -1
    m.subUrl = ""
    m.detail = invalid
    m.decision = invalid
    m.subs = invalid
    m.offset = 0
    m.ended = false
    m.ticks = 0
    m.skipFor = ""
    m.dismissed = {}
    m.upNext = invalid
    m.upNextLeft = 0
    m.upNextCancelled = false
    m.skip.visible = false
    m.card.visible = false
    m.menu.visible = false
    m.status.text = "Starting " + item.title + "…"
    m.status.visible = true

    id = item.id.toStr()
    m.detailTask = ApiGet("/api/items/" + id, "onDetail")
    m.subsTask = ApiGet("/api/items/" + id + "/subtitles", "onSubs")
    askDecision()
end sub

function streamParams() as string
    q = "profile=roku"
    if m.claims <> "" then q = q + "&can=" + m.claims
    if m.audio >= 0 then q = q + "&audio=" + m.audio.toStr()
    return q
end function

sub askDecision()
    m.decision = invalid
    m.decisionTask = ApiGet("/api/items/" + m.item.id.toStr() + "/playback?" + streamParams(), "onDecision")
end sub

sub onDetail(e as object)
    if not e.getRoSGNode().isSameNode(m.detailTask) then return
    r = ApiAnswer(e)
    if r.code <> 200 or type(r.data) <> "roAssociativeArray" then
        fail("Could not load this title: " + r.error)
        return
    end if
    m.detail = r.data
    maybeStart()
end sub

sub onSubs(e as object)
    if not e.getRoSGNode().isSameNode(m.subsTask) then return
    r = ApiAnswer(e)
    m.subs = []
    if r.code = 200 and type(r.data) = "roAssociativeArray" and type(r.data.tracks) = "roArray" then
        for each t in r.data.tracks
            if t.available = true then m.subs.push(t)
        end for
    end if
    maybeStart()
end sub

sub onDecision(e as object)
    if not e.getRoSGNode().isSameNode(m.decisionTask) then return
    r = ApiAnswer(e)
    if r.code <> 200 or type(r.data) <> "roAssociativeArray" or type(r.data.decision) <> "roAssociativeArray" then
        fail("The server would not say how to play this: " + r.error)
        return
    end if
    m.decision = r.data.decision
    maybeStart()
end sub

sub maybeStart()
    if m.detail = invalid or m.decision = invalid or m.subs = invalid then return
    at = m.at
    if at < 0 then at = ResumeAt(m.detail)
    play(at)
end sub

' play starts the stream at a position in milliseconds.
sub play(atMs as dynamic)
    d = m.decision
    id = m.item.id.toStr()
    base = m.global.lcBase
    sec = Int(atMs / 1000)

    c = CreateObject("roSGNode", "ContentNode")
    c.title = m.detail.title
    if d.method = "direct" then
        m.hls = false
        m.offset = 0
        c.url = base + "/api/stream/" + id
        c.streamFormat = directFormat(m.detail.container)
        if sec > 0 then c.PlayStart = sec
    else
        m.hls = true
        m.offset = sec
        q = streamParams()
        if sec > 0 then q = q + "&t=" + sec.toStr()
        c.url = base + "/api/stream/" + id + "/hls/index.m3u8?" + q
        c.streamFormat = "hls"
    end if
    c.SubtitleTracks = subtitleTracks()

    m.video.content = c
    m.video.control = "play"
    m.timer.control = "start"
    ' A chosen subtitle survives a restart (a seek or an audio change).
    if m.subUrl <> "" then showSubtitle(m.subUrl)
end sub

function directFormat(container as dynamic) as string
    if container = invalid then return "mp4"
    c = LCase(container)
    if c = "matroska" or c = "mkv" or c = "webm" then return "mkv"
    if c = "mpegts" or c = "ts" then return "ts"
    if c = "mp3" or c = "flac" or c = "wav" or c = "aac" then return c
    return "mp4"
end function

' The subtitles as side-loaded WebVTT, shifted by the stream's offset so a
' resumed conversion's cues land where its clock is (docs/api.md).
function subtitleTracks() as object
    out = []
    for each t in m.subs
        out.push({ Language: subLanguage(t), TrackName: subUrl(t), Description: subLabel(t) })
    end for
    return out
end function

function subUrl(t as object) as string
    u = m.global.lcBase + "/api/items/" + m.item.id.toStr() + "/subtitles/" + t.key + ".vtt"
    if m.offset > 0 then u = u + "?t=" + m.offset.toStr()
    return u
end function

function subLanguage(t as object) as string
    if t.language <> invalid and t.language <> "" then return t.language
    return "und"
end function

function subLabel(t as object) as string
    l = ""
    if t.label <> invalid then l = t.label
    if l = "" then l = subLanguage(t)
    if t.forced = true then l = l + " (forced)"
    if t.source = "external" then l = l + " · file"
    return l
end function

' ---- while playing -----------------------------------------------------

function positionMs() as dynamic
    return Int((m.offset + m.video.position) * 1000)
end function

sub onState()
    s = m.video.state
    if s = "playing" then
        m.status.visible = false
        applyAudioTrack()
    else if s = "error" then
        detail = m.video.errorMsg
        if m.video.errorStr <> invalid and m.video.errorStr <> "" then detail = detail + " (" + m.video.errorStr + ")"
        fail("This did not play: " + detail)
        ' A revoked key looks like any other failed segment from here. Ask the
        ' server once, so a 401 reaches MainScene and the TV goes to pairing.
        m.probe = ApiSend("GET", "/api/items/" + m.item.id.toStr(), invalid, "onAnswered")
    else if s = "finished" then
        if endedEarly() then return
        finished()
    end if
end sub

sub onTick()
    if m.video.state <> "playing" then return
    m.ticks = m.ticks + 1
    if m.ticks mod 5 = 0 then saveProgress(positionMs(), false)
    atMs = positionMs()
    m.lastMs = atMs
    markers(atMs)
    upNext(atMs)
end sub

sub saveProgress(posMs as dynamic, watched as boolean)
    if m.item = invalid then return
    t = ApiSend("PUT", "/api/items/" + m.item.id.toStr() + "/progress", { position_ms: posMs, watched: watched }, "onAnswered")
    ' Held so the request is not collected before it is sent.
    m.saves.push(t)
    if m.saves.count() > 4 then m.saves.shift()
end sub

' onAnswered stops the picture when the key has been revoked; ApiAnswer then
' tells MainScene, which forgets the key and shows pairing.
sub onAnswered(e as object)
    if e.getData().code = 401 then stopStream()
    ApiAnswer(e)
end sub

' ---- skip intro and credits --------------------------------------------

' The markers a player may offer (docs/api.md, markers): an intro with an end;
' credits from blackdetect-gated on anything, from blackdetect or ending-audio
' on an episode only.
function markerList() as object
    if m.detail = invalid or type(m.detail.markers) <> "roArray" then return []
    return m.detail.markers
end function

function creditsAt() as dynamic
    for each k in markerList()
        if k.kind = "credits" and k.start_ms <> invalid then
            if k.source = "blackdetect-gated" then return k.start_ms
            if m.item.kind = "episode" and (k.source = "blackdetect" or k.source = "ending-audio") then return k.start_ms
        end if
    end for
    return invalid
end function

sub markers(atMs as dynamic)
    want = ""
    for each k in markerList()
        if k.kind = "intro" and k.start_ms <> invalid and k.end_ms <> invalid then
            if atMs >= k.start_ms and atMs < k.end_ms - 1000 then
                want = "intro"
                m.skipTarget = k.end_ms
            end if
        end if
    end for
    credits = creditsAt()
    if want = "" and credits <> invalid and atMs >= credits and nextItem() = invalid then want = "credits"

    if want = "" or m.dismissed.DoesExist(want) then
        if m.skip.visible then hideSkip()
        return
    end if
    if m.skipFor = want then return
    m.skipFor = want
    if want = "intro" then m.skip.text = "Skip intro" else m.skip.text = "Skip credits"
    m.skip.visible = true
    if not m.menu.visible and not m.card.visible then m.skip.setFocus(true)
end sub

sub hideSkip()
    focused = m.skip.hasFocus()
    m.skip.visible = false
    m.skipFor = ""
    if focused then m.video.setFocus(true)
end sub

sub onSkip()
    what = m.skipFor
    hideSkip()
    if what = "intro" then
        seekTo(m.skipTarget)
    else
        ' Skipping a film's credits is finishing it.
        finished()
    end if
end sub

' seekTo moves to a real position in milliseconds. A direct file and a
' listed HLS stream seek in place; before an HLS stream's start it restarts.
sub seekTo(posMs as dynamic)
    sec = posMs / 1000
    if m.hls and sec < m.offset then
        restartAt(posMs)
        return
    end if
    m.video.seek = sec - m.offset
end sub

sub restartAt(posMs as dynamic)
    stopStream()
    play(posMs)
end sub

' ---- Up Next -----------------------------------------------------------

function nextItem() as dynamic
    if m.item = invalid then return invalid
    for i = 0 to m.queue.count() - 2
        if m.queue[i].id = m.item.id then return m.queue[i + 1]
    end for
    return invalid
end function

sub upNext(atMs as dynamic)
    n = nextItem()
    if n = invalid or m.upNextCancelled then return
    if m.card.visible then
        m.upNextLeft = m.upNextLeft - 1
        if m.upNextLeft <= 0 then
            playNext()
        else
            m.cardHead.text = "Up next in " + m.upNextLeft.toStr()
        end if
        return
    end if
    ' At the credits, or twenty seconds from the end when there are none.
    credits = creditsAt()
    due = false
    if credits <> invalid then
        due = atMs >= credits
    else if m.detail.duration_ms <> invalid then
        due = atMs >= m.detail.duration_ms - 20000
    end if
    if not due then return
    m.upNext = n
    m.upNextLeft = 10
    m.cardHead.text = "Up next in 10"
    m.cardTitle.text = n.title
    sub_ = ""
    if n.season <> invalid and n.episode <> invalid then sub_ = "Season " + n.season.toStr() + ", episode " + n.episode.toStr()
    m.cardSub.text = sub_
    if m.skip.visible then hideSkip()
    m.card.visible = true
    m.nextNow.setFocus(true)
end sub

sub hideCard(cancel as boolean)
    focused = m.nextNow.hasFocus()
    m.card.visible = false
    if cancel then m.upNextCancelled = true
    if focused then m.video.setFocus(true)
end sub

sub onNextNow()
    playNext()
end sub

sub playNext()
    n = nextItem()
    if n = invalid then
        leave()
        return
    end if
    saveProgress(positionMs(), false)
    hideCard(false)
    begin(n, -1)
    m.video.setFocus(true)
end sub

' ---- audio and subtitles ------------------------------------------------

function audioStreams() as object
    out = []
    if m.detail = invalid or type(m.detail.streams) <> "roArray" then return out
    for each s in m.detail.streams
        if s.kind = "audio" then out.push(s)
    end for
    return out
end function

function audioLabel(s as object) as string
    parts = []
    if s.title <> invalid and s.title <> "" then
        parts.push(s.title)
    else if s.language <> invalid and s.language <> "" then
        parts.push(UCase(s.language))
    end if
    if s.codec <> invalid then parts.push(UCase(s.codec))
    if s.channels <> invalid then
        if s.channels = 6 then
            parts.push("5.1")
        else if s.channels = 8 then
            parts.push("7.1")
        else if s.channels = 2 then
            parts.push("stereo")
        else if s.channels = 1 then
            parts.push("mono")
        end if
    end if
    return parts.Join(" · ")
end function

' The audio track playing now: the chosen one, else the file's default.
function currentAudio() as integer
    if m.audio >= 0 then return m.audio
    tracks = audioStreams()
    for each s in tracks
        if s.default = true then return s.index
    end for
    if tracks.count() > 0 then return tracks[0].index
    return -1
end function

sub openMenu()
    if m.detail = invalid then return
    m.menuItems = []
    content = CreateObject("roSGNode", "ContentNode")
    focusAt = 0
    tracks = audioStreams()
    if tracks.count() > 1 then
        now = currentAudio()
        for each s in tracks
            if s.index = now then focusAt = m.menuItems.count()
            addChoice(content, "Audio: " + audioLabel(s), s.index = now, { kind: "audio", index: s.index })
        end for
    end if
    addChoice(content, "Subtitles: off", m.subUrl = "", { kind: "suboff" })
    for each t in m.subs
        u = subUrl(t)
        addChoice(content, "Subtitles: " + subLabel(t), m.subUrl = u, { kind: "sub", url: u })
    end for
    m.choices.content = content
    m.choices.jumpToItem = focusAt
    m.menu.visible = true
    m.choices.setFocus(true)
end sub

sub addChoice(content as object, title as string, current as boolean, what as object)
    c = content.createChild("ContentNode")
    c.title = title
    c.addFields({ shown: current })
    m.menuItems.push(what)
end sub

sub closeMenu()
    m.menu.visible = false
    m.video.setFocus(true)
end sub

sub onChoice()
    what = m.menuItems[m.choices.itemSelected]
    closeMenu()
    if what = invalid then return
    if what.kind = "suboff" then
        m.subUrl = ""
        m.video.globalCaptionMode = "Off"
    else if what.kind = "sub" then
        m.subUrl = what.url
        showSubtitle(what.url)
    else if what.kind = "audio" and what.index <> currentAudio() then
        ' The server decides again for the new track: Dolby may play as it
        ' is where DTS is converted. Pick up where it was.
        m.audio = what.index
        m.at = positionMs()
        stopStream()
        m.status.text = "Changing the audio…"
        m.status.visible = true
        askDecision()
    end if
end sub

sub showSubtitle(url as string)
    m.video.subtitleTrack = url
    m.video.globalCaptionMode = "On"
end sub

' A direct-played file carries every audio track; the Video node chooses
' among them itself. Its list is in the file's order.
sub applyAudioTrack()
    if m.hls or m.audio < 0 then return
    k = 0
    for each s in audioStreams()
        if s.index = m.audio then exit for
        k = k + 1
    end for
    tracks = m.video.availableAudioTracks
    if type(tracks) = "roArray" and k < tracks.count() then m.video.audioTrack = tracks[k].Track
end sub

' ---- stopping ----------------------------------------------------------

sub fail(message as string)
    m.timer.control = "stop"
    m.status.text = message + " Press Back to return."
    m.status.visible = true
end sub

' endedEarly is true when Roku says "finished" far from the end: the server
' restarted or the stream broke, and the player gave up. Marking that watched
' would send the next Play back to 0:00, so keep the place and say so.
function endedEarly() as boolean
    if m.detail = invalid or m.detail.duration_ms = invalid or m.lastMs < 0 then return false
    if m.detail.duration_ms - m.lastMs <= 60000 then return false
    saveProgress(m.lastMs, false)
    stopStream()
    fail("The stream stopped at " + ClockText(m.lastMs) + ". Your place is kept.")
    return true
end function

sub finished()
    if m.ended then return
    m.ended = true
    if m.detail <> invalid and m.detail.duration_ms <> invalid then
        saveProgress(m.detail.duration_ms, true)
    else
        saveProgress(positionMs(), true)
    end if
    if nextItem() <> invalid and not m.upNextCancelled then
        playNext()
    else
        leave()
    end if
end sub

' stopStream stops the picture and, for HLS, the server's encode: it writes
' segments whether anybody reads them or not (DELETE /api/stream/{id}/hls).
sub stopStream()
    m.timer.control = "stop"
    m.video.control = "stop"
    if m.hls and m.item <> invalid then
        m.stopTask = ApiSend("DELETE", "/api/stream/" + m.item.id.toStr() + "/hls", invalid, "")
    end if
    m.hls = false
end sub

sub leave()
    if m.item <> invalid and not m.ended and m.video.position > 0 then saveProgress(positionMs(), false)
    stopStream()
    m.top.done = true
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if m.menu.visible then
        if key = "back" or key = "options" or key = "left" then
            closeMenu()
            return true
        end if
        return false
    end if
    if key = "back" then
        leave()
        return true
    end if
    ' Any other key while a prompt holds focus puts it away and leaves the
    ' picture to the remote: a skip you did not want, or an Up Next you did not.
    if m.skip.visible and m.skip.hasFocus() then
        m.dismissed[m.skipFor] = true
        hideSkip()
        return true
    end if
    if m.card.visible and m.nextNow.hasFocus() then
        hideCard(true)
        return true
    end if
    ' Down opens the menu. The Video node keeps * (options) for Roku's own
    ' caption menu and never passes it up, which the TV proved on build 7.
    if key = "down" or key = "options" then
        openMenu()
        return true
    end if
    return false
end function
