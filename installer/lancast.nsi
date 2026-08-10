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
!define MUI_FINISHPAGE_RUN "$INSTDIR\LANcast-Client.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open LANcast"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Open LANcast in my browser instead"
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION OpenInBrowser
!insertmacro MUI_PAGE_FINISH

Function OpenInBrowser
  ; -browser is the documented opt-out. Launched detached so the installer can
  ; finish rather than waiting on a client the user is about to use.
  Exec '"$INSTDIR\LANcast-Client.exe" -browser'
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
  File "..\third_party\webview2\x64\WebView2Loader.dll"
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
  Delete "$INSTDIR\WebView2Loader.dll"
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
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "LANcast"

  DeleteRegKey HKLM "${UNINST_KEY}"

  ; The library data in %ProgramData%\LANcast is deliberately left in place — an
  ; uninstall must not destroy a user's library. Removing it is a manual choice.
SectionEnd
