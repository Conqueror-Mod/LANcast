; LANcast Windows installer (ADR 0022, ADR 0080). Lays down the one LANcast
; executable, registers it as the server's service when asked to (ServicePage),
; and puts a Start-menu and desktop shortcut to it.
;
; Built in the release pipeline with makensis; VERSION is passed in:
;   makensis -DVERSION=0.3.0 lancast.nsi
; It expects LANcast.exe beside this script (the release job copies it from
; the goreleaser output).

!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif

Name "LANcast"
OutFile "LANcast-Setup-${VERSION}.exe"
Unicode true
InstallDir "$PROGRAMFILES64\LANcast"
; Program Files and service registration both need elevation.
RequestExecutionLevel admin

; Branding: the app icon for the installer, and the L mark on the welcome/finish
; side panel. The panel is only 164px wide, so the wordmark's tagline is
; illegible there — the tagline is set as real text below instead, which stays
; crisp at any DPI.
!define MUI_ICON "..\assets\lancast.ico"
!define MUI_UNICON "..\assets\lancast.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "welcome.bmp"

; The title control is a fixed height and renders large and bold, so anything
; that wraps past two lines is clipped. The name goes there alone; the tagline
; leads the body text, which has room and a smaller font.
!define MUI_WELCOMEPAGE_TITLE "Welcome to LANcast"
!define MUI_WELCOMEPAGE_TEXT "Your gateway to everything on your LAN.$\r$\n$\r$\nSetup will install LANcast on your computer.$\r$\n$\r$\nLANcast can run as a background service that starts with Windows; Setup asks which you want. When Setup finishes, open the LANcast shortcut to get to your library — no terminal required.$\r$\n$\r$\nClick Next to continue."

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
Page custom ServicePage ServicePageLeave
!insertmacro MUI_PAGE_INSTFILES
; $PLUGINSDIR is where OpenInBrowser writes its temporary shortcut, and NSIS
; only creates it on demand — for a page that uses a plugin, which none here
; does. Asking for it explicitly is the documented way.
!define MUI_CUSTOMFUNCTION_GUIINIT MakePluginsDir
; Two ways to finish, because the two are genuinely different applications of
; the same client and the installer is the one place a person picks.
;
; The first checkbox opens LANcast's own window, which is the default and the
; better experience: it owns its close button, it pins the server's certificate,
; and against a LAN-bound server it does not show the warning a browser must.
;
; The second is the browser, offered rather than hidden. It is the right answer
; on a machine without the WebView2 runtime, and it is what someone used to the
; old behaviour will look for. SHOWREADME is repurposed to carry it — NSIS gives
; the finish page exactly two checkboxes and no way to add a third.
; Both checkboxes go through a function so the launch happens as the person,
; not as this elevated installer. See LaunchAsUser.
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION StartLANcast
!define MUI_FINISHPAGE_RUN_TEXT "Start LANcast"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Start LANcast and open it in my browser instead"
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION OpenInBrowser
!insertmacro MUI_PAGE_FINISH

; The tray and the window are one process now (ADR 0080), so starting LANcast
; starts both: there is no separate tray to start first any more, which is
; what this space used to explain.
;
; Started as the person, not as the installer.
;
; This installer runs elevated (RequestExecutionLevel admin, because it writes
; to Program Files and installs a service) and `Exec` hands its own token to
; whatever it starts. Finishing into the client therefore left LANcast running
; **as administrator** until the next time it was closed and reopened — a
; privilege the client has no use for, and one Windows enforces in ways that
; read as bugs rather than as security: drag and drop from Explorer stops
; working, and input from any lower-integrity process is silently discarded.
; That last one is how this was found, a week after it started happening.
;
; `explorer.exe` is the way out without a third-party plugin (the UAC and
; ShellExecAsUser plugins are not in the NSIS the release job installs): it is
; already running as the signed-in user, so anything it opens inherits **that**
; token. It takes a file to open rather than a command line, which is why each
; launch goes through a shortcut — the two the installer just wrote already
; carry the right arguments, and the browser one is made here for the purpose.
;
; $9 is the shortcut to open.
Function MakePluginsDir
  InitPluginsDir
FunctionEnd

; A named label, never a relative jump.
;
; This function shipped in v0.9.29 as `IfErrors 0 +3`, and +3 is one past the
; end of it: +1 is the MessageBox, +2 is the Return that FunctionEnd compiles
; to, and +3 is the first instruction of whatever function follows — StartTray.
; So on **success**, which is the ordinary path, it skipped its own return and
; fell into StartTray, which calls this function again. Unbounded recursion,
; starting a LANcast on every pass: thirty in under thirty seconds, and the
; machine went down with them.
;
; It compiled without a warning, and `makensis -WX` in CI proved only that. A
; relative jump cannot be checked by the assembler because every target is
; arithmetically valid; a label that does not exist is a compile error. That is
; the whole argument for never counting instructions in this file.
Function LaunchAsUser
  ClearErrors
  Exec 'explorer.exe "$9"'
  IfErrors 0 launched
    ; No Explorer to borrow a token from — a locked-down shell, or a machine
    ; where it has crashed. Starting it elevated is worse than not starting it.
    MessageBox MB_OK|MB_ICONINFORMATION "LANcast is installed. Open it from the Start menu."
  launched:
FunctionEnd

Function StartLANcast
  StrCpy $9 "$SMPROGRAMS\LANcast\LANcast.lnk"
  Call LaunchAsUser
FunctionEnd

Function OpenInBrowser
  ; -browser is the documented opt-out, and no shortcut carries it, so one is
  ; written for this launch. $PLUGINSDIR because it is temporary by definition
  ; and removed when the installer exits; the Sleep is what keeps it alive long
  ; enough for Explorer to read it, which takes milliseconds.
  CreateShortcut "$PLUGINSDIR\LANcast in browser.lnk" "$INSTDIR\LANcast.exe" \
    "-browser" "$INSTDIR\LANcast.exe" 0 SW_SHOWNORMAL "" \
    "Open LANcast in the default browser"
  StrCpy $9 "$PLUGINSDIR\LANcast in browser.lnk"
  Call LaunchAsUser
  Sleep 1500
FunctionEnd

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LANcast"

; Quietly runs a command whose failure is expected and ignored, without
; showing its output.
;
; These are the "stop it if it is there" steps: sc.exe and taskkill both report
; an error when there is nothing to stop, and run through ExecToLog that report
; went into the details pane of every install. A clean install of a machine
; with nothing running showed three lines of "ERROR: The process ... not
; found", which reads as a failure that is not one. nsExec::Exec runs hidden and
; pushes the exit code; it is popped and discarded, because a failure here is
; the expected case. ExecToLog pushed one too, and nothing popped it.
!macro Quietly cmd
  nsExec::Exec `${cmd}`
  Pop $0
!macroend

;; ---- the service, as a choice ------------------------------------------------
;
; Not everybody needs one. A service is what keeps libraries reachable from
; other devices with nobody signed in; someone who only watches on this PC is
; served just as well by the server the app starts for itself while it is open
; (cmd/lancast, ensureServer), and spared a background process at every boot.
;
; The choice is not free once a library exists, and that is what this page is
; mostly about. The two modes keep their database in different places: the
; service is pinned to the machine-wide folder (service.DefaultDataDir), and
; the app's own server takes the per-user one. So:
;
;   - A library already in the machine-wide folder stays a service. That
;     folder belongs to the service account, and a server started as the
;     person gets a read-only database there (serverwait.go).
;   - A library already in the person's own folder stays without one. A
;     service would open the machine-wide folder and show an empty library,
;     which reads as everything having been deleted.
;
; Moving a library between the two is a real migration, not a checkbox, and
; Setup does not attempt it. The box is shown greyed with the reason instead.
;
; Silent installs take /SERVICE or /NOSERVICE, subject to the same locks.
; Remembered in the uninstall key so an upgrade asks with the last answer.
Var ServiceChoice
Var ServiceLocked
Var ServiceBox

Function .onInit
  StrCpy $ServiceChoice "1"
  StrCpy $ServiceLocked ""
  ReadRegStr $0 HKLM "${UNINST_KEY}" "Service"
  ${If} $0 == "0"
    StrCpy $ServiceChoice "0"
  ${EndIf}
  ${GetParameters} $1
  ClearErrors
  ${GetOptions} $1 "/NOSERVICE" $0
  ${IfNot} ${Errors}
    StrCpy $ServiceChoice "0"
  ${EndIf}
  ClearErrors
  ${GetOptions} $1 "/SERVICE" $0
  ${IfNot} ${Errors}
    StrCpy $ServiceChoice "1"
  ${EndIf}
  ReadEnvStr $0 "ProgramData"
  ${If} ${FileExists} "$0\LANcast\lancast.db"
    StrCpy $ServiceChoice "1"
    StrCpy $ServiceLocked "service"
  ${ElseIf} ${FileExists} "$APPDATA\LANcast\lancast.db"
    StrCpy $ServiceChoice "0"
    StrCpy $ServiceLocked "user"
  ${EndIf}
FunctionEnd

Function ServicePage
  !insertmacro MUI_HEADER_TEXT "How LANcast runs" "Choose whether the server runs as a Windows service."
  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == error
    Abort
  ${EndIf}
  ${NSD_CreateLabel} 0 0 100% 52u "As a service, the LANcast server starts with Windows and keeps your libraries available to your other devices whether or not anybody is signed in.$\r$\n$\r$\nWithout one, the server runs only while the LANcast app is open, and your library is kept in your own user folder. Start LANcast at login, from its tray icon, if you want it there whenever you are."
  Pop $0
  ${NSD_CreateCheckbox} 0 60u 100% 12u "&Install LANcast as a Windows service"
  Pop $ServiceBox
  ${If} $ServiceChoice == "1"
    ${NSD_Check} $ServiceBox
  ${EndIf}
  ${If} $ServiceLocked == "service"
    EnableWindow $ServiceBox 0
    ${NSD_CreateLabel} 0 80u 100% 40u "Your library is already kept by the LANcast service, in the machine-wide folder, so LANcast stays a service. Without one, the app could not write to that library."
    Pop $0
  ${ElseIf} $ServiceLocked == "user"
    EnableWindow $ServiceBox 0
    ${NSD_CreateLabel} 0 80u 100% 40u "Your library is kept in your own user folder. A service keeps its library in the machine-wide folder and would start with an empty one, so LANcast stays as it is."
    Pop $0
  ${EndIf}
  nsDialogs::Show
FunctionEnd

Function ServicePageLeave
  ${NSD_GetState} $ServiceBox $0
  ${If} $0 == ${BST_CHECKED}
    StrCpy $ServiceChoice "1"
  ${Else}
    StrCpy $ServiceChoice "0"
  ${EndIf}
FunctionEnd

Section "LANcast"
  ; Upgrade path. An earlier install registered the service from differently
  ; named executables (lancastd.exe / lancast.exe); leaving that registration
  ; behind would orphan a service pointing at a file this install removes. Stop
  ; and delete any existing service by name first — sc reports an error when the
  ; service is absent, which is fine and ignored, and is not shown.
  !insertmacro Quietly 'sc.exe stop lancastd'
  !insertmacro Quietly 'sc.exe delete lancastd'

  ; Stop anything still running, under either the old or the current names.
  ; Deleting the files is not enough: a tray client from the previous version
  ; keeps running, holds the single-instance lock so the new one will not start,
  ; and leaves the user with an old build they cannot see they are using.
  ; taskkill reports an error when nothing matches, which is fine and ignored,
  ; and is not shown (Quietly).
  !insertmacro Quietly 'taskkill /F /IM lancast.exe'
  !insertmacro Quietly 'taskkill /F /IM lancastd.exe'
  !insertmacro Quietly 'taskkill /F /IM LANcast-Client.exe'
  !insertmacro Quietly 'taskkill /F /IM LANcast-Server.exe'
  !insertmacro Quietly 'taskkill /F /IM LANcast.exe'

  ; Files an earlier LANcast laid down that nothing runs any more, by name.
  ;
  ; By name and never by sweep: this folder is LANcast's, but a pattern broad
  ; enough to be convenient is broad enough to one day take something that was
  ; not stale. The two programs became one in ADR 0080, so LANcast-Server.exe
  ; and LANcast-Client.exe go, with the `.old` / `.old.<stamp>` copies the
  ; in-app updater leaves while it swaps a running file (the new program's own
  ; sweep only knows its own name). Older names still from before 0.4.
  Delete "$INSTDIR\lancastd.exe"
  Delete "$INSTDIR\lancast.exe"
  Delete "$INSTDIR\LANcast-Server.exe"
  Delete "$INSTDIR\LANcast-Client.exe"
  Delete "$INSTDIR\LANcast-Server.exe.old*"
  Delete "$INSTDIR\LANcast-Client.exe.old*"

  ; The old server tray's log (applog.TrayFileName and its one rotation), in
  ; the two places that tray wrote it: the machine-wide data directory its
  ; shortcut pinned, and the per-user one. Exact names only: those folders
  ; hold the library's database, and nothing else in them is touched.
  ReadEnvStr $0 "ProgramData"
  Delete "$0\LANcast\lancast-tray.log"
  Delete "$0\LANcast\lancast-tray.log.1"
  Delete "$APPDATA\LANcast\lancast-tray.log"
  Delete "$APPDATA\LANcast\lancast-tray.log.1"

  SetOutPath "$INSTDIR"
  File "LANcast.exe"
  ; Microsoft's WebView2 loader shim, which the client's window mode calls into
  ; (ADR 0023 stage 1). It has to sit beside LANcast.exe: the client
  ; resolves it by name, and the alternative — the upstream binding's habit of
  ; embedding a copy and mapping it from memory — is a blob in our binary and a
  ; technique that trips AV, so LANcast ships Microsoft's signed file instead
  ; (internal/webview2/PROVENANCE.md).
  ;
  ; Its absence is survivable: the client says the install is incomplete and
  ; opens the browser. That is a worse app, not a broken one — which is why
  ; this is a File line and not an abort.
  ; The face worker (ADR 0052). Three megabytes, and bundled rather than
  ; downloaded for one reason: the wire format between it and the server is
  ; a contract, and a worker whose version can drift from the server driving
  ; it is a support question nobody can answer from a log. Pinning both ends
  ; to the same release is free.
  ;
  ; It is useless alone — the models and the ONNX runtime it needs are about
  ; 115 MB and are fetched on request by somebody who wants the feature.
  ; Most installs never do, and pay nothing for it.
  File "lancast-faces.exe"
  File "..\third_party\webview2\x64\WebView2Loader.dll"

  ; libmpv, the desktop client's player (ADR 0067). LGPL, built by this project
  ; rather than taken from mpv's own Windows builds, which are GPL and so cannot
  ; ship beside a commercially licensed binary (ADR 0069).
  ;
  ; Built and hashed out of band -- third_party/libmpv/build.sh -- and placed
  ; there by the release job, because a large decoder does not belong in git.
  ; The client falls back to the browser player when it is absent, so an install
  ; without it is degraded rather than broken.
  ; Compiled in only when the DLL is actually there: CI builds this script with
  ; placeholder executables and no payload, and the release job fetches the
  ; published build first (third_party/libmpv/fetch.sh). An installer without
  ; it is a working installer whose client uses the browser player, which is
  ; the same fallback a machine that loses the file gets.
  !if /FileExists "..\third_party\libmpv\out\libmpv-2.dll"
    File "..\third_party\libmpv\out\libmpv-2.dll"
    File /oname=libmpv-LICENSE.txt "..\third_party\libmpv\LICENSE.LGPL"
  !else
    ; Said out loud, not warned: CI compiles this with -WX and a warning here
    ; would turn "no payload staged" into a red build for the script being right.
    !echo "libmpv-2.dll is absent: this installer ships without the native player"
  !endif
  File "..\README.md"
  File "..\LICENSE"

  ; Register the server as a service pinned to the machine-wide data dir, and
  ; start it — when that was the choice (ServicePage). `service install`
  ; refuses an unset --data, so the pin is enforced. Without it the app starts
  ; its own server when it opens. Any earlier service was removed above, so
  ; "no" really is no service.
  ${If} $ServiceChoice == "1"
    nsExec::ExecToLog '"$INSTDIR\LANcast.exe" service install'
    nsExec::ExecToLog '"$INSTDIR\LANcast.exe" service start'
  ${EndIf}

  ; One shortcut, because there is one program (ADR 0080). It opens the
  ; window and its tray; the server is the service, started above.
  ;
  ; The two older entries go: "LANcast Client" pointed at a file this install
  ; removed, and "LANcast Server" started a tray that no longer exists.
  ;
  ; (ReadEnvStr, used above for ProgramData, and never $%ProgramData%: NSIS
  ; expands $%VAR% at COMPILE time on the Linux runner, where it is empty, and
  ; that once shipped a literal "$%ProgramData%" into a shortcut — v0.4.1.)
  CreateDirectory "$SMPROGRAMS\LANcast"
  Delete "$SMPROGRAMS\LANcast\LANcast Client.lnk"
  Delete "$SMPROGRAMS\LANcast\LANcast Server.lnk"
  CreateShortcut "$SMPROGRAMS\LANcast\LANcast.lnk" "$INSTDIR\LANcast.exe" \
    "" "$INSTDIR\LANcast.exe" 0 SW_SHOWNORMAL "" "Open LANcast"
  CreateShortcut "$DESKTOP\LANcast.lnk" "$INSTDIR\LANcast.exe"

  ; Start at login, carried across the rename.
  ;
  ; Somebody who had it on has a "LANcast" run-key value naming
  ; LANcast-Client.exe, which this install just deleted: left alone it is an
  ; error dialog every morning. Rewritten to the new program, still -hidden,
  ; so the choice survives; absent, nothing is written, because a login entry
  ; is somebody's choice and never a default. "LANcast Tray" started the old
  ; server tray and is removed outright: the app migrates it too, but a
  ; machine that skips straight to this release may never have run that app.
  ; HKCU is the installing user's hive; other accounts' apps migrate their own
  ; entries when they start.
  ReadRegStr $1 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LANcast"
  StrCmp $1 "" loginDone
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LANcast" '"$INSTDIR\LANcast.exe" -hidden'
  loginDone:
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LANcast Tray"

  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "LANcast"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\LANcast.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "LANcast"
  WriteRegStr HKLM "${UNINST_KEY}" "Service" $ServiceChoice
SectionEnd

Section "Uninstall"
  ; Stop and remove the service before deleting its binary, then stop anything
  ; still running interactively — a tray client holds its executable open, and
  ; Delete silently fails on a file in use. Quietly, because an install that
  ; chose no service (ServicePage) has none to stop, and that is not a failure.
  !insertmacro Quietly '"$INSTDIR\LANcast.exe" service stop'
  !insertmacro Quietly '"$INSTDIR\LANcast.exe" service uninstall'
  !insertmacro Quietly 'taskkill /F /IM LANcast.exe'
  !insertmacro Quietly 'taskkill /F /IM LANcast-Client.exe'
  !insertmacro Quietly 'taskkill /F /IM LANcast-Server.exe'

  Delete "$INSTDIR\LANcast.exe"
  Delete "$INSTDIR\LANcast.exe.old*"
  ; Older names too, in case this uninstalls over a half-finished upgrade.
  Delete "$INSTDIR\LANcast-Server.exe"
  Delete "$INSTDIR\LANcast-Client.exe"
  Delete "$INSTDIR\lancast-faces.exe"
  Delete "$INSTDIR\WebView2Loader.dll"
  Delete "$INSTDIR\libmpv-2.dll"
  Delete "$INSTDIR\libmpv-LICENSE.txt"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\uninstall.exe"
  ; The window mode keeps a browser profile — cookies, cache, local storage —
  ; under the user's config directory, not here. Left in place on uninstall,
  ; the same way a browser's profile survives: it holds the session and any
  ; local settings, and removing it is a "clear my data" action rather than
  ; something an uninstaller should decide (see docs/desktop-lifecycle-plan.md).
  RMDir "$INSTDIR"

  ; The one shortcut, and the two older entries in case an upgrade left them.
  Delete "$SMPROGRAMS\LANcast\LANcast.lnk"
  Delete "$SMPROGRAMS\LANcast\LANcast Client.lnk"
  Delete "$SMPROGRAMS\LANcast\LANcast Server.lnk"
  RMDir "$SMPROGRAMS\LANcast"
  Delete "$DESKTOP\LANcast.lnk"

  ; "Open when Windows starts" is a per-user run key the client writes for
  ; itself. Left behind, it points at an executable this uninstaller just
  ; deleted — which is a login-time error dialog every morning, forever, with
  ; nothing obvious to blame (docs/desktop-lifecycle-plan.md).
  ;
  ; HKCU here is the *uninstalling* user's hive, so this clears it for whoever
  ; ran the uninstall. An elevated uninstall started from another account, and
  ; other accounts on a shared machine, are not reached — the entry is per user
  ; and there is no machine-wide place to sweep. Those users' clients rewrite or
  ; clear their own key the next time they touch the setting.
  ; Both run-key values. The client and the server tray each own one since
  ; they stopped sharing a name (internal/autostart); deleting only the old
  ; one would leave an entry pointing at a removed executable, which is a
  ; login error dialog every morning with nothing obvious to blame.
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LANcast"
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LANcast Tray"

  DeleteRegKey HKLM "${UNINST_KEY}"

  ; The library data in %ProgramData%\LANcast is deliberately left in place — an
  ; uninstall must not destroy a user's library. Removing it is a manual choice.
SectionEnd
