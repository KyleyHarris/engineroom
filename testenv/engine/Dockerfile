# A throwaway engine for the journeys: sshd, git and Zellij, and nothing of anyone's real setup.
#
# It exists so the journeys exercise real SSH between a client and an engine (docs/decisions.md
# D-003) without touching a real machine. The test suite builds the program for Linux, copies
# it in, injects a freshly generated public key at start, and throws the container away after.
FROM debian:bookworm-slim

ARG ZELLIJ_VERSION=0.45.1

RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-server git ca-certificates curl procps socat \
 && rm -rf /var/lib/apt/lists/*

# The musl build is static, so it runs on the slim image without extra libraries.
RUN arch="$(uname -m)" \
 && curl -fsSL "https://github.com/zellij-org/zellij/releases/download/v${ZELLIJ_VERSION}/zellij-${arch}-unknown-linux-musl.tar.gz" \
    | tar -xz -C /usr/local/bin zellij \
 && zellij --version

# One unprivileged developer account; the journeys log in as it with a key, never a password.
RUN useradd --create-home --shell /bin/bash dev \
 && mkdir -p /home/dev/.ssh /home/dev/bin /run/sshd \
 && chown -R dev:dev /home/dev \
 && chmod 700 /home/dev/.ssh

RUN printf '%s\n' \
    'PasswordAuthentication no' \
    'KbdInteractiveAuthentication no' \
    'PermitRootLogin no' \
    'PubkeyAuthentication yes' \
    'AllowUsers dev' \
    'SetEnv PATH=/home/dev/bin:/usr/local/bin:/usr/bin:/bin' \
    > /etc/ssh/sshd_config.d/engine.conf

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
