FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends python3 sudo \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin pistar-wizard \
 && useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin mmdvm
