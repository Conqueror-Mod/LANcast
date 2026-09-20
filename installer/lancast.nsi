; LANcast Windows installer (ADR 0022). Lays down both executables, registers the
; server as a service, and puts a Start-menu/desktop shortcut to the launcher.
;
; Built in the release pipeline with makensis; VERSION is passed in:
;   makensis -DVERSION=0.3.0 lancast.nsi
; It expects lancastd.exe and lancast.exe beside this script (the release job
; copies them from the goreleaser output).

!include "MUI2.nsh"

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
!define MUI_WELCOMEPAGE_TEXT "Your gateway to everything on your LAN.$\r$\n$\r$\nSetup will install LANcast on your computer.$\r$\n$\r$\nLANcast runs as a background service and starts with Windows. When Setup finishes, open the LANcast shortcut to get to your library — no terminal required.$\r$\n$\r$\nClick Next to continue."

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
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
; Both checkboxes go through a function rather than straight to the client,
; because starting LANcast means starting the *tray* first. See StartLANcast.
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION StartLANcast
!define MUI_FINISHPAGE_RUN_TEXT "Start LANcast"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Start LANcast and open it in my browser instead"
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION OpenInBrowser
!insertmacro MUI_PAGE_FINISH

; The tray is the top of the hierarchy, and neither finish option used to start
; it.
;
; The service is installed and running by this point, so LANcast *works* — but
; the tray is how a person reaches it: start it, stop it, open it, check for an
; update, decide whether it launches at login. Finishing the installer straight
; into the client window left the notification area empty, so the only visible
; LANcast was a window, and closing that window looked like closing LANcast.
;
; So both checkboxes start the tray first and then the surface the person asked
; for. Starting it twice is harmless — the tray holds a lock of its own and a
; second launch opens the UI instead of adding an icon.
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

Function StartTray
  ; The Start Menu shortcut, which already pins -data to the machine-wide
  ; directory: without that the tray reads a relative directory and opens a
  ; second database beside the install, which is the failure v0.4.1 was about.
  StrCpy $9 "$SMPROGRAMS\LANcast\LANcast Server.lnk"
  Call LaunchAsUser
  ; A moment for the icon to appear before the window opens on top of it. Not a
  ; synchronisation — nothing depends on the order — but a person watching sees
  ; the tray populate rather than a window arriving from nowhere.
  Sleep 400
FunctionEnd

Function StartLANcast
  Call StartTray
  StrCpy $9 "$SMPROGRAMS\LANcast\LANcast Client.lnk"
  Call LaunchAsUser
FunctionEnd

Function OpenInBrowser
  Call StartTray
  ; -browser is the documented opt-out, and no shortcut carries it, so one is
  ; written for this launch. $PLUGINSDIR because it is temporary by definition
  ; and removed when the installer exits; the Sleep is what keeps it alive long
  ; enough for Explorer to read it, which takes milliseconds.
  CreateShortcut "$PLUGINSDIR\LANcast in browser.lnk" "$INSTDIR\LANcast-Client.exe" \
    "-browser" "$INSTDIR\LANcast-Client.exe" 0 SW_SHOWNORMAL "" \
    "Open LANcast in the default browser"
  StrCpy $9 "$PLUGINSDIR\LANcast in browser.lnk"
  Call LaunchAsUser
  Sleep 1500
FunctionEnd

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LANcast"

Section "LANcast"
  ; Upgrade path. An earlier install registered the service from differently
  ; named executables (lancastd.exe / lancast.exe); leaving that registration
  ; behind would orphan a service pointing at a file this install removes. Stop
  ; and delete any existing service by name first — sc reports an error when the
  ; service is absent, which is fine and ignored.
  nsExec::ExecToLog 'sc.exe stop lancastd'
  nsExec::ExecToLog 'sc.exe delete lancastd'

  ; Stop anything still running, under either the old or the current names.
  ; Deleting the files is not enough: a tray client from the previous version
  ; keeps running, holds the single-instance lock so the new one will not start,
  ; and leaves the user with an old build they cannot see they are using.
  ; taskkill reports an error when nothing matches, which is fine and ignored.
  nsExec::ExecToLog 'taskkill /F /IM lancast.exe'
  nsExec::ExecToLog 'taskkill /F /IM lancastd.exe'
  nsExec::ExecToLog 'taskkill /F /IM LANcast-Client.exe'
  nsExec::ExecToLog 'taskkill /F /IM LANcast-Server.exe'

  Delete "$INSTDIR\lancastd.exe"
  Delete "$INSTDIR\lancast.exe"

  SetOutPath "$INSTDIR"
  File "LANcast-Server.exe"
  File "LANcast-Client.exe"
  ; Microsoft's WebView2 loader shim, which the client's window mode calls into
  ; (ADR 0023 stage 1). It has to sit beside LANcast-Client.exe: the client
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
  ; start it. `service install` refuses an unset --data, so the pin is enforced.
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service install'
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service start'

  ; Two shortcuts, because there are two programs and which one you launched
  ; changes what happens. A single "LANcast" entry pointing at the client made
  ; that invisible: when the service was not running the client silently
  ; started its own server, and there was no way to launch the server on its
  ; own or to tell from the Start menu which you had.
  ;
  ; The desktop shortcut stays the client — that is the one to double-click.
  CreateDirectory "$SMPROGRAMS\LANcast"
  ; Upgrading from a single-shortcut install: remove the old entry so it does
  ; not sit beside the two new ones pointing at the same program.
  Delete "$SMPROGRAMS\LANcast\LANcast.lnk"
  CreateShortcut "$SMPROGRAMS\LANcast\LANcast Client.lnk" "$INSTDIR\LANcast-Client.exe" \
    "" "$INSTDIR\LANcast-Client.exe" 0 SW_SHOWNORMAL "" "Open LANcast"
  ; The server pinned to the same machine-wide data directory the service uses,
  ; so starting it by hand cannot quietly open a second, per-user database.
  ;
  ; ReadEnvStr, not $%ProgramData%. NSIS expands $%VAR% at COMPILE time from the
  ; compiler's own environment, and this installer is compiled by makensis on a
  ; Linux runner where ProgramData does not exist — so it expanded to nothing and
  ; shipped the literal text "$%ProgramData%\LANcast" into the shortcut. The
  ; server then read that as a relative directory and opened a second database
  ; beside the install, which is exactly the failure this -data argument exists
  ; to prevent (v0.4.1). The original intent — "the environment variable,
  ; expanded on the target machine" — was right; only the mechanism was wrong,
  ; and ReadEnvStr is the one that actually runs there.
  ;
  ; Not SetShellVarContext all + $APPDATA, which is the other documented route:
  ; that setting also moves $SMPROGRAMS and $DESKTOP to the all-users folders, so
  ; using it here would quietly relocate every shortcut this installer writes.
  ReadEnvStr $0 "ProgramData"
  CreateShortcut "$SMPROGRAMS\LANcast\LANcast Server.lnk" "$INSTDIR\LANcast-Server.exe" \
    'tray -data "$0\LANcast"' "$INSTDIR\LANcast-Server.exe" 0 SW_SHOWNORMAL "" \
    "Run the LANcast server in the system tray"
  CreateShortcut "$DESKTOP\LANcast.lnk" "$INSTDIR\LANcast-Client.exe"

  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "LANcast"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\LANcast-Client.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "LANcast"
SectionEnd

Section "Uninstall"
  ; Stop and remove the service before deleting its binary, then stop anything
  ; still running interactively — a tray client holds its executable open, and
  ; Delete silently fails on a file in use.
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service stop'
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service uninstall'
  nsExec::ExecToLog 'taskkill /F /IM LANcast-Client.exe'
  nsExec::ExecToLog 'taskkill /F /IM LANcast-Server.exe'

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

  ; Includes the pre-0.4.1 single shortcut, so upgrading from an older install
  ; does not leave a stale "LANcast" entry beside the two new ones.
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
