# Regenerate from the repository root:
#   mise run build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo.tape
# VHS also requires tmux, ttyd, and trash on PATH.

Output docs/demo.gif

Require tmux
Require trash

Set Shell "bash"
Set FontSize 18
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
Type `touch "$WORKLEASE_HOME/.owner"`
Enter
Type `export WORKLEASE_SESSION_ID=a WORKLEASE_AGENT_ID=worker-a PS1='worker-a $ '`
Enter
Type `SHELL=/bin/bash tmux -f /dev/null new-session -s worklease-demo /bin/bash --noprofile --norc`
Enter
Sleep 1s
Type `tmux set-option -g status off; tmux set-option -g default-command 'exec /bin/bash --noprofile --norc'; tmux split-window -h`
Enter
Sleep 1s
Type `export WORKLEASE_SESSION_ID=b WORKLEASE_AGENT_ID=worker-b PS1='worker-b $ '; clear`
Enter
Ctrl+B
Type `o`
Type `clear`
Enter
Sleep 1s
Show

Type `worklease acquire -r task:demo | head -1`
Enter
Sleep 2s
Type `echo 'Working on task:demo...'`
Enter
Sleep 2s

Ctrl+B
Type `o`
Type `worklease acquire -r task:demo 2>&1 | head -1`
Enter
Sleep 3s

Ctrl+B
Type `o`
Type `worklease release | head -1`
Enter
Sleep 2s

Ctrl+B
Type `o`
Type `worklease acquire -r task:demo | head -1`
Enter
Sleep 2s
Type `echo 'Claim acquired — worker-b can start'`
Enter
Sleep 3s
Type `worklease release | head -1`
Enter
Sleep 3s

Hide
Ctrl+B
Type `:kill-session`
Enter
Sleep 1s
Type `cleanup_worklease_demo() { local worklease_demo_target worklease_temp_root; worklease_demo_target=$(realpath "$WORKLEASE_HOME"); worklease_temp_root=$(realpath "${TMPDIR:-/tmp}"); case "$worklease_demo_target" in "$worklease_temp_root"/worklease-demo.*) test -f "$worklease_demo_target/.owner" && trash "$worklease_demo_target" ;; esac; }; cleanup_worklease_demo`
Enter
