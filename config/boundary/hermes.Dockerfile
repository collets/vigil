# Source and uv.lock come only from the pinned git archive prepared by
# scripts/boundary/build-hermes.py. No host homes or credentials enter context.
FROM ghcr.io/astral-sh/uv:0.11.6-python3.13-trixie@sha256:b3c543b6c4f23a5f2df22866bd7857e5d304b67a564f4feab6ac22044dde719b AS sqlite_build
# Match the pinned Hermes source's SQLite requirement rather than using the
# standalone Python distribution's older bundled library.
RUN curl -fsSL --connect-timeout 15 --max-time 120 https://sqlite.org/2026/sqlite-autoconf-3530400.tar.gz -o /tmp/sqlite.tar.gz && \
    echo '0e9483900e92cd5de8fd48d16bf9200145a61f7fd5be542a5ac81d8a9516eb9c  /tmp/sqlite.tar.gz' | sha256sum -c - && \
    tar -xzf /tmp/sqlite.tar.gz -C /tmp && cd /tmp/sqlite-autoconf-3530400 && \
    CFLAGS='-O2 -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_THREADSAFE=1' ./configure --prefix=/opt/sqlite-fixed --disable-static && \
    make -j2 && make install

FROM ghcr.io/astral-sh/uv:0.11.6-python3.13-trixie@sha256:b3c543b6c4f23a5f2df22866bd7857e5d304b67a564f4feab6ac22044dde719b
ENV UV_PYTHON=/usr/local/bin/python3 UV_PYTHON_DOWNLOADS=never
COPY --from=sqlite_build /opt/sqlite-fixed/lib/ /opt/sqlite-fixed/lib/
RUN ln -sf libsqlite3.so.3.53.4 /opt/sqlite-fixed/lib/libsqlite3.so.0
ENV LD_LIBRARY_PATH=/opt/sqlite-fixed/lib
RUN apt-get update && apt-get install -y --no-install-recommends git ripgrep util-linux && rm -rf /var/lib/apt/lists/*
ADD hermes.tar /opt/hermes/
WORKDIR /opt/hermes
RUN uv sync --frozen --no-install-project --no-dev
RUN .venv/bin/python -c "import sqlite3; assert sqlite3.sqlite_version_info >= (3, 53, 4); sqlite3.connect(':memory:').execute('CREATE VIRTUAL TABLE evidence USING fts5(content)')"
COPY vigil-guardian vigil-worker /opt/vigil/
RUN mkdir -p /native /work /control /relay
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=/opt/hermes HOME=/native HERMES_HOME=/native/hermes HERMES_DISABLE_LAZY_INSTALLS=1
WORKDIR /work
ENTRYPOINT ["/opt/vigil/vigil-guardian"]
