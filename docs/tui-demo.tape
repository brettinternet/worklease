# Regenerate from the repository root:
#   mise run build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/tui-demo.tape
# VHS also requires tmux, ttyd, and trash on PATH.

Output docs/tui-demo.gif

Require tmux
Require trash

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 560
Set Padding 24
Set Margin 16
Set MarginFill "#11111b"
Set BorderRadius 10
Set WindowBar "ColorfulRight"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 25ms
Set Framerate 20

Hide
Type `export PATH="$PWD/bin:$PATH"`
Enter
Type `export WORKLEASE_DEMO_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/worklease-tui-demo.XXXXXX")`
Enter
Type `touch "$WORKLEASE_DEMO_ROOT/.owner"`
Enter
Type `export XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/config" XDG_STATE_HOME="$WORKLEASE_DEMO_ROOT/state" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/home" WORKLEASE_AGENT_ID=demo-agent WORKLEASE_SESSION_ID=demo PS1='demo $ '`
Enter
Type `clear`
Enter
Show

Type `worklease acquire -r task:demo | head -1`
Enter
Sleep 2s
Type `worklease`
Enter
Sleep 3s
Enter
Sleep 3s
Type `u`
Sleep 2s
Enter
Sleep 3s
Type `R`
Sleep 1s
Enter
Sleep 2s
Enter
Sleep 3s
Type `q`
Sleep 1s
Enter
Sleep 1s

Hide
Type `cleanup_worklease_tui_demo() { local worklease_demo_target worklease_temp_root; worklease_demo_target=$(realpath "$WORKLEASE_DEMO_ROOT"); worklease_temp_root=$(realpath "${TMPDIR:-/tmp}"); case "$worklease_demo_target" in "$worklease_temp_root"/worklease-tui-demo.*) test -f "$worklease_demo_target/.owner" && trash "$worklease_demo_target" ;; esac; }; cleanup_worklease_tui_demo`
Enter
