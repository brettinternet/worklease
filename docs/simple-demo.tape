# Regenerate from the repository root:
#   mise run sync
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/simple-demo.tape
# VHS also requires ttyd and trash on PATH.

Output docs/simple-demo.gif

Require trash

Set Shell "bash"
Set FontSize 20
Set Width 1200
Set Height 800
Set Padding 24
Set Margin 16
Set MarginFill "#11111b"
Set BorderRadius 10
Set WindowBar "ColorfulRight"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Framerate 20

Hide
Type `export PATH="$PWD/.venv/bin:$PATH"`
Enter
Type `export WORKLEASE_HOME=$(mktemp -d "${TMPDIR:-/tmp}/worklease-simple-demo.XXXXXX")`
Enter
Type `touch "$WORKLEASE_HOME/.owner"; export LEASE="$WORKLEASE_HOME/my-task.lease" WORKLEASE_AGENT_ID=alice`
Enter
Type `export PS1='$ '`
Enter
Type `clear`
Enter
Show

Type `worklease acquire -r my-task --lease-file "$LEASE"`
Enter
Sleep 2s
Type `echo 'Doing protected work...'`
Enter
Sleep 2s
Type `worklease release --lease-file "$LEASE" -m done`
Enter
Sleep 3s

Hide
Type `cleanup_worklease_simple_demo() { local worklease_demo_target worklease_temp_root; worklease_demo_target=$(realpath "$WORKLEASE_HOME"); worklease_temp_root=$(realpath "${TMPDIR:-/tmp}"); case "$worklease_demo_target" in "$worklease_temp_root"/worklease-simple-demo.*) test -f "$worklease_demo_target/.owner" && trash "$worklease_demo_target" ;; esac; }; cleanup_worklease_simple_demo`
Enter
