FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends php8.2-cli php8.2-xml python3 sudo && rm -rf /var/lib/apt/lists/* \
 && useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin mmdvm
