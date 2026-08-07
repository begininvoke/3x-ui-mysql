# ========================================================
# Stage: Builder
# ========================================================

FROM golang:1.26-alpine AS builder

WORKDIR /app
ARG TARGETARCH

RUN apk --no-cache --update add \
    curl \
    unzip

COPY . .

ENV CGO_ENABLED=0

RUN go build -ldflags "-w -s" -o build/x-ui main.go
RUN ./DockerInit.sh "$TARGETARCH"


# ========================================================
# Stage: Final Image of 3x-ui
# ========================================================

FROM alpine

ENV TZ=Asia/Tehran

WORKDIR /app

RUN apk add --no-cache --update \
    ca-certificates \
    tzdata \
    fail2ban \
    bash \
    curl \
    openssl

COPY --from=builder /app/build/ /app/
COPY --from=builder /app/DockerEntrypoint.sh /app/
COPY --from=builder /app/x-ui.sh /usr/bin/x-ui

# Configure fail2ban
RUN rm -f /etc/fail2ban/jail.d/alpine-ssh.conf \
    && cp /etc/fail2ban/jail.conf /etc/fail2ban/jail.local \
    && sed -i "s/^\[ssh\]$/&\nenabled = false/" /etc/fail2ban/jail.local \
    && sed -i "s/^\[sshd\]$/&\nenabled = false/" /etc/fail2ban/jail.local \
    && sed -i "s/#allowipv6 = auto/allowipv6 = auto/g" /etc/fail2ban/fail2ban.conf

RUN chmod +x \
    /app/DockerEntrypoint.sh \
    /app/x-ui \
    /usr/bin/x-ui

ENV XUI_IN_DOCKER="true"
ENV XUI_MAIN_FOLDER="/app"

# Recommended on Railway:
ENV XUI_ENABLE_FAIL2BAN="false"

ENV XUI_PORT="2053"
ENV XUI_DB_TYPE=""
ENV XUI_DB_DSN=""

EXPOSE 2053

# IMPORTANT:
# Do NOT add Docker VOLUME here.
# Railway Volume will be mounted to /etc/x-ui externally.

CMD ["./x-ui"]
ENTRYPOINT ["/app/DockerEntrypoint.sh"]
