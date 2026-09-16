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
Type `touch "$WORKLEASE_DEMO_ROOT/.owner"; mkdir -m 700 "$WORKLEASE_DEMO_ROOT/server" "$WORKLEASE_DEMO_ROOT/server-state" "$WORKLEASE_DEMO_ROOT/admin-config" "$WORKLEASE_DEMO_ROOT/admin-home" "$WORKLEASE_DEMO_ROOT/client-config" "$WORKLEASE_DEMO_ROOT/client-home"`
Enter
Type `export XDG_STATE_HOME="$WORKLEASE_DEMO_ROOT/server-state"`
Enter
Type `worklease server init --guided --server-config "$WORKLEASE_DEMO_ROOT/server/server.yaml" --bootstrap-invite-file "$WORKLEASE_DEMO_ROOT/server/admin.invite" --listen 127.0.0.1:18443 --endpoint https://127.0.0.1:18443 --transport tls --admitted-prefix coordination:`
Enter
Type `worklease serve --server-config "$WORKLEASE_DEMO_ROOT/server/server.yaml" > "$WORKLEASE_DEMO_ROOT/server.log" 2>&1 & export WORKLEASE_SERVER_PID=$!`
Enter
Sleep 1s
Type `install -m 600 "$WORKLEASE_DEMO_ROOT/server/admin.invite" "$WORKLEASE_DEMO_ROOT/admin.invite"`
Enter
Type `XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/admin-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/admin-home" worklease enroll --profile team --invite-file "$WORKLEASE_DEMO_ROOT/admin.invite" --label administrator`
Enter
Type `XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/admin-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/admin-home" worklease invite issue --role write --invite-file "$WORKLEASE_DEMO_ROOT/client.invite" --label team`
Enter
Type `install -m 600 "$WORKLEASE_DEMO_ROOT/client.invite" "$WORKLEASE_DEMO_ROOT/client-config/client.invite"`
Enter
Type `XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/client-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/client-home" worklease enroll --invite-file "$WORKLEASE_DEMO_ROOT/client-config/client.invite" --label client`
Enter
Type `export XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/client-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/client-home" WORKLEASE_PROFILE=team WORKLEASE_SESSION_ID=client WORKLEASE_AGENT_ID=client PS1='client@remote $ '`
Enter
Type `SHELL=/bin/bash tmux -f /dev/null new-session -s worklease-remote-demo /bin/bash --noprofile --norc`
Enter
Sleep 1s
Type `tmux set-option -g status off; tmux set-option -g default-command 'exec /bin/bash --noprofile --norc'; tmux split-window -h`
Enter
Sleep 1s
Type `export XDG_CONFIG_HOME="$WORKLEASE_DEMO_ROOT/admin-config" WORKLEASE_HOME="$WORKLEASE_DEMO_ROOT/admin-home" WORKLEASE_PROFILE=team WORKLEASE_SESSION_ID=admin WORKLEASE_AGENT_ID=admin PS1='admin@remote $ '; clear`
Enter
Ctrl+B
Type `o`
Type `clear`
Enter
Sleep 1s
Show

Type `worklease doctor --resource coordination:demo`
Enter
Sleep 2s
Type `worklease acquire --resource coordination:demo`
Enter
Sleep 2s

Ctrl+B
Type `o`
Type `worklease list --full`
Enter
Sleep 3s

Ctrl+B
Type `o`
Type `worklease heartbeat`
Enter
Sleep 2s
Type `worklease release --reason done`
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
