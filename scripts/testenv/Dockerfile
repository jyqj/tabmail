# Isolated validation tooling only. Never build/run the company stack here.
# Supply GO_VERSION from go.mod; Node major and PostgreSQL match the CI jobs.
ARG GO_VERSION
FROM golang:${GO_VERSION}-bookworm AS go-toolchain
FROM node:22.23.1-bookworm AS node-toolchain
FROM python:3.12-bookworm AS python-toolchain
FROM postgres:16-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
      gcc libc6-dev python3 git ca-certificates libstdc++6 \
    && rm -rf /var/lib/apt/lists/*
# company_snapshot.py requires tarfile's safe extraction filter. Do not replace
# it with an unsafe fallback just to support Debian's older system Python.
COPY --from=python-toolchain /usr/local/bin/python3.12 /usr/local/bin/python3.12
COPY --from=python-toolchain /usr/local/lib/python3.12 /usr/local/lib/python3.12
COPY --from=python-toolchain /usr/local/lib/libpython3.12.so.1.0 /usr/local/lib/libpython3.12.so.1.0
RUN ln -s python3.12 /usr/local/bin/python3 && ldconfig \
    && python3 -c "import tarfile; assert hasattr(tarfile, 'data_filter')"
COPY --from=go-toolchain /usr/local/go /usr/local/go
COPY --from=node-toolchain /usr/local/bin/node /usr/local/bin/node
COPY --from=node-toolchain /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
ENV PATH="/usr/local/go/bin:${PATH}" \
    PYTHONDONTWRITEBYTECODE=1 \
    GOSUMDB=sum.golang.org \
    GOTOOLCHAIN=local \
    GOPATH=/go \
    GOCACHE=/go/build-cache
WORKDIR /src
ENTRYPOINT []
CMD ["go", "version"]
