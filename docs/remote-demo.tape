# Regenerate from the repository root:
#   mise run build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/remote-demo.tape
# VHS also requires tmux, ttyd, and trash on PATH.

Output docs/remote-demo.gif

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
Type `export WORKLEASE_DEMO_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/worklease-remote-demo.XXXXXX")`
Enter
Type `touch "$WORKLEASE_DEMO_ROOT/.owner"; mkdir -m 700 "$WORKLEASE_DEMO_ROOT/config" "$WORKLEASE_DEMO_ROOT/state"`
Enter
Type `export XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/config" XDG_STATE_HOME="$WORKLEASE_DEMO_ROOT/state"`
Enter
Type `worklease --json server init > "$WORKLEASE_DEMO_ROOT/init.json"`
Enter
Type `export WORKLEASE_AUTHORITY_ID=$(sed -E 's/.*"authorityId":"([^"]+)".*/\1/' "$WORKLEASE_DEMO_ROOT/init.json")`
Enter
Type `worklease serve > "$WORKLEASE_DEMO_ROOT/server.log" 2>&1 & export WORKLEASE_SERVER_PID=$!`
Enter
Sleep 1s
Type `worklease profile add demo --endpoint http://127.0.0.1:8443 --authority-id "$WORKLEASE_AUTHORITY_ID" --allow-insecure-http`
Enter
Type `worklease enroll --profile demo --invite-file "$XDG_CONFIG_HOME/worklease/bootstrap.invite"`
Enter
Type `export WORKLEASE_PROFILE=demo WORKLEASE_SESSION_ID=a WORKLEASE_AGENT_ID=worker-a PS1='worker-a@remote $ '`
Enter
Type `SHELL=/bin/bash tmux -f /dev/null new-session -s worklease-remote-demo /bin/bash --noprofile --norc`
Enter
Sleep 1s
Type `tmux set-option -g status off; tmux set-option -g default-command 'exec /bin/bash --noprofile --norc'; tmux split-window -h`
Enter
Sleep 1s
Type `export WORKLEASE_SESSION_ID=b WORKLEASE_AGENT_ID=worker-b PS1='worker-b@remote $ '; clear`
Enter
Ctrl+B
Type `o`
Type `clear`
Enter
Sleep 1s
Show

Type `echo 'Using worklease serve at http://127.0.0.1:8443'`
Enter
Sleep 2s
Type `worklease acquire -r coordination:demo`
Enter
Sleep 2s
Type `echo 'Working on coordination:demo...'`
Enter
Sleep 2s

Ctrl+B
Type `o`
Type `worklease acquire -r coordination:demo`
Enter
Sleep 3s

Ctrl+B
Type `o`
Type `worklease release`
Enter
Sleep 2s

Ctrl+B
Type `o`
Type `worklease acquire -r coordination:demo`
Enter
Sleep 2s
Type `echo 'Remote claim acquired — worker-b can start'`
Enter
Sleep 3s
Type `worklease release`
Enter
Sleep 3s

Hide
Ctrl+B
Type `:kill-session`
Enter
Sleep 1s
Type `kill "$WORKLEASE_SERVER_PID"; wait "$WORKLEASE_SERVER_PID" 2>/dev/null || true`
Enter
Type `cleanup_worklease_remote_demo() { local worklease_demo_target worklease_temp_root; worklease_demo_target=$(realpath "$WORKLEASE_DEMO_ROOT"); worklease_temp_root=$(realpath "${TMPDIR:-/tmp}"); case "$worklease_demo_target" in "$worklease_temp_root"/worklease-remote-demo.*) test -f "$worklease_demo_target/.owner" && trash "$worklease_demo_target" ;; esac; }; cleanup_worklease_remote_demo`
Enter
