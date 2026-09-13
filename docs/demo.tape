# Regenerate from the repository root:
#   mise run build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo.tape
# VHS also requires ttyd and trash on PATH.

Output docs/demo.gif

Require trash

Set Shell "bash"
Set FontSize 22
Set Width 1400
Set Height 720
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
Type `export WORKLEASE_HOME=$(mktemp -d "${TMPDIR:-/tmp}/worklease-demo.XXXXXX")`
Enter
Type `touch "$WORKLEASE_HOME/.owner"; export PS1='demo $ '`
Enter
Type `clear`
Enter
Show

Type `worklease acquire -r task:demo --session a -a worker-a | head -1`
Enter
Sleep 2s
Type `echo 'worker-a is handling task:demo'`
Enter
Sleep 2s

Type `worklease acquire -r task:demo --session b -a worker-b 2>&1 | head -1`
Enter
Sleep 3s

Type `worklease release --session a --reason done | head -1`
Enter
Sleep 2s
Type `worklease acquire -r task:demo --session b -a worker-b | head -1`
Enter
Sleep 2s
Type `worklease list`
Enter
Sleep 3s
Type `worklease release --session b --reason done | head -1`
Enter
Sleep 3s

Hide
Type `cleanup_worklease_demo() { local worklease_demo_target worklease_temp_root; worklease_demo_target=$(realpath "$WORKLEASE_HOME"); worklease_temp_root=$(realpath "${TMPDIR:-/tmp}"); case "$worklease_demo_target" in "$worklease_temp_root"/worklease-demo.*) test -f "$worklease_demo_target/.owner" && trash "$worklease_demo_target" ;; esac; }; cleanup_worklease_demo`
Enter
