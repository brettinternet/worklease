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
Set TypingSpeed 20ms
Set Framerate 20

Hide
Type `export PATH="$PWD/bin:$PATH"`
Enter
Type `export WORKLEASE_DEMO_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/worklease-remote-demo.XXXXXX")`
Enter
Type `touch "$WORKLEASE_DEMO_ROOT/.owner"`
Enter
Type `export XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/server-config" XDG_STATE_HOME="$WORKLEASE_DEMO_ROOT/server-state"`
Enter
# Local secure default: TLS loopback, pinned certificate, coordination: namespace.
Type `worklease server init`
Enter
Type `worklease serve > "$WORKLEASE_DEMO_ROOT/server.log" 2>&1 & export WORKLEASE_SERVER_PID=$!`
Enter
Sleep 1s
Type `XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/worker-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/worker-home" worklease enroll --invite-file "$WORKLEASE_DEMO_ROOT/server-config/worklease/bootstrap.invite"`
Enter
Type `kill "$WORKLEASE_SERVER_PID"; wait "$WORKLEASE_SERVER_PID" 2>/dev/null || true`
Enter
Type `export WORKLEASE_SESSION_ID=server WORKLEASE_AGENT_ID=server PS1='server $ '`
Enter
Type `SHELL=/bin/bash tmux -f /dev/null new-session -s worklease-remote-demo /bin/bash --noprofile --norc`
Enter
Sleep 1s
Type `tmux set-option -g status off; tmux set-option -g default-command 'exec /bin/bash --noprofile --norc'; tmux split-window -h`
Enter
Sleep 1s
Type `export XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/worker-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/worker-home" WORKLEASE_SESSION_ID=worker WORKLEASE_AGENT_ID=worker PS1='worker $ '; clear`
Enter
Ctrl+B
Type `o`
Type `clear`
Enter
Sleep 1s
Show

Type `worklease serve`
Enter
Sleep 2s

Ctrl+B
Type `o`
Type `worklease acquire -r coordination:demo`
Enter
Sleep 3s
Type `worklease status`
Enter
Sleep 3s
Type `worklease release`
Enter
Sleep 3s

# LAN: worklease server init --listen 0.0.0.0:8443 --endpoint https://HOST:8443 --confirm-non-loopback
# Customize next with setup flags, WORKLEASE_SERVER_CONFIG, or server.yaml keys.

Hide
Ctrl+B
Type `:kill-session`
Enter
Sleep 1s
Type `cleanup_worklease_remote_demo() { local worklease_demo_target worklease_temp_root; worklease_demo_target=$(realpath "$WORKLEASE_DEMO_ROOT"); worklease_temp_root=$(realpath "${TMPDIR:-/tmp}"); case "$worklease_demo_target" in "$worklease_temp_root"/worklease-remote-demo.*) test -f "$worklease_demo_target/.owner" && trash "$worklease_demo_target" ;; esac; }; cleanup_worklease_remote_demo`
Enter
