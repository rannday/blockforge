# OpenRC
## conf.d
```bash
MC_USER="minecraft"
MC_GROUP="minecraft"

MC_DIR="/opt/minecraft/instances/<name>"
MC_START_SCRIPT="./run.sh"

JAVA_HOME="/usr/lib/jvm/temurin-21-jdk-amd64"

TMUX_SOCK="minecraft-<name>"
TMUX_TARGET="<name>"

MC_STOP_TIMEOUT="120"

MC_INSTALLER="/usr/local/bin/blockforge"

MC_UPDATE_WARN_SECONDS="300"
```
## init.d
```bash
#!/sbin/openrc-run

name="Minecraft Server"
description="<name> Minecraft Modpack Server"

extra_commands="console update"
description_console="Attach to the Minecraft tmux console"
description_update="Run Blockforge update workflow"

depend() {
  need mountall.sh networking
  use rsyslog
}

tmux_has_session() {
  su -s /bin/sh -c "tmux -L '${TMUX_SOCK}' has-session -t '${RC_SVCNAME}'" "${MC_USER}" 2>/dev/null
}

mc_send() {
  su -s /bin/sh -c \
    "tmux -L '${TMUX_SOCK}' send-keys -t '${TMUX_TARGET}' C-u \"\$1\" Enter" \
    "${MC_USER}" sh "$1"
}

pack_version() {
  if [ -f "${MC_DIR}/.blockforge/pack-version.txt" ]; then
    sed -n '1p' "${MC_DIR}/.blockforge/pack-version.txt" | tr -d '[:space:]'
    return 0
  fi

  echo ""
}

latest_pack_version() {
  su -s /bin/sh -c "
    \"${MC_INSTALLER}\" --check --dir \"${MC_DIR}\" \
      | awk -F: '/^[[:space:]]*version:/ { gsub(/[[:space:]]/, \"\", \$2); print \$2; exit }'
  " "${MC_USER}"
}

start_pre() {
  command -v tmux >/dev/null 2>&1 || {
    eerror "tmux not found"
    return 1
  }

  [ -n "${MC_USER:-}" ] || {
    eerror "MC_USER is not set"
    return 1
  }

  [ -n "${MC_GROUP:-}" ] || {
    eerror "MC_GROUP is not set"
    return 1
  }

  [ -n "${MC_DIR:-}" ] || {
    eerror "MC_DIR is not set"
    return 1
  }

  [ -n "${MC_START_SCRIPT:-}" ] || {
    eerror "MC_START_SCRIPT is not set"
    return 1
  }

  checkpath -d -o "${MC_USER}:${MC_GROUP}" -m 0770 "${MC_DIR}"

  if [ ! -x "${MC_DIR}/${MC_START_SCRIPT#./}" ]; then
    eerror "Start script is not executable: ${MC_DIR}/${MC_START_SCRIPT#./}"
    return 1
  fi
}

start() {
  if tmux_has_session; then
    ewarn "${name} is already running"
    return 0
  fi

  ebegin "Starting ${name}"

  su -s /bin/sh -c "
    cd \"${MC_DIR}\" || exit 1
    exec tmux -L \"${TMUX_SOCK}\" new-session -d -s \"${RC_SVCNAME}\" \
      \"export JAVA_HOME='${JAVA_HOME}'; export PATH='${JAVA_HOME}/bin':\\\$PATH; exec '${MC_DIR}/${MC_START_SCRIPT#./}'\"
  " "${MC_USER}"

  eend $?
}

stop() {
  local i

  ebegin "Stopping ${name}"

  if ! tmux_has_session; then
    eend 0
    return 0
  fi

  mc_send "stop"

  for i in $(seq 1 "${MC_STOP_TIMEOUT:-60}"); do
    if ! tmux_has_session; then
      eend 0
      return 0
    fi
    sleep 1
  done

  ewarn "Server did not stop cleanly, killing tmux session"

  su -s /bin/sh -c \
    "tmux -L '${TMUX_SOCK}' kill-session -t '${RC_SVCNAME}'" \
    "${MC_USER}" 2>/dev/null

  eend 0
}

status() {
  if tmux_has_session; then
    einfo "${name} is running in tmux session ${RC_SVCNAME}"
    return 0
  fi

  einfo "${name} is not running"
  return 3
}

console() {
  if ! tmux_has_session; then
    eerror "${name} is not running"
    return 1
  fi

  exec su -s /bin/sh -c \
    "tmux -L '${TMUX_SOCK}' attach -t '${RC_SVCNAME}'" \
    "${MC_USER}"
}

update() {
  local before_version after_version latest_version was_running warn_seconds

  [ -n "${MC_INSTALLER:-}" ] || {
    eerror "MC_INSTALLER is not set"
    return 1
  }

  [ -x "${MC_INSTALLER}" ] || {
    eerror "Installer is not executable: ${MC_INSTALLER}"
    return 1
  }

  ebegin "Checking Varda installer"
  latest_version="$(latest_pack_version)" || {
    eend 1 "Installer check failed"
    return 1
  }
  eend 0

  [ -n "${latest_version}" ] || {
    eerror "Could not determine latest manifest version"
    return 1
  }

  before_version="$(pack_version)"

  einfo "Installed: ${before_version:-none}"
  einfo "Latest:    ${latest_version}"

  if [ -n "${before_version}" ] && [ "${before_version}" = "${latest_version}" ]; then
    einfo "Already up to date"
    return 0
  fi

  was_running=0
  if tmux_has_session; then
    was_running=1
    warn_seconds="${MC_UPDATE_WARN_SECONDS:-300}"

    einfo "Server is running, warning players before update"

    if [ "${warn_seconds}" -ge 300 ]; then
      mc_send "say Server update starting, shutting down in 5 minutes..."
      sleep 240
      mc_send "say Server shutting down in 1 minute..."
      sleep 50
      mc_send "say Server shutting down in 10 seconds..."
      sleep 10
    elif [ "${warn_seconds}" -gt 0 ]; then
      mc_send "say Server update starting, shutting down in ${warn_seconds} seconds..."
      sleep "${warn_seconds}"
    fi

    if ! /etc/init.d/"${RC_SVCNAME}" stop; then
      eerror "Failed to stop server cleanly"
      return 1
    fi
  fi

  if tmux_has_session; then
    eerror "Server still appears to be running after stop"
    return 1
  fi

  ebegin "Running Blockforge"

  if ! su -s /bin/sh -c "
    cd \"${MC_DIR}\" || exit 1
    exec \"${MC_INSTALLER}\" --dir \"${MC_DIR}\"
  " "${MC_USER}"; then
    eend 1 "Installer failed"
    return 1
  fi

  eend 0

  after_version="$(pack_version)"
  einfo "Updated: ${before_version:-none} -> ${after_version:-unknown}"

  if [ "${was_running}" -eq 1 ]; then
    einfo "Starting server"
    if ! /etc/init.d/"${RC_SVCNAME}" start; then
      eerror "Update completed, but restart failed"
      return 1
    fi
  fi

  einfo "Update complete"
  return 0
}
```
