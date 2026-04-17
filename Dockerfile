# ---- Build UI----
FROM node:22-alpine AS node
WORKDIR /app
COPY ui .
RUN yarn install
RUN yarn run build

# ---- Build Go----
FROM golang:1.25-alpine AS golang
WORKDIR /app
COPY --from=node /app/dist ui/dist
COPY . .
RUN apk update && apk add git
RUN CGO_ENABLED=0 go build -ldflags "-s -w"

# ---- Release ----
FROM alpine
LABEL maintainer="Stefaweb <stefanod83@gmail.com>"
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata
COPY --from=golang /app/swirl .
COPY --from=golang /app/config config/
# SWIRL_CONTAINER_ID is the operator-visible hook for Swirl's self-deploy
# protection. When Swirl is running inside a Docker container you can
# optionally set this to the container's ID so a stack deploy that would
# replace the Swirl container is refused up-front. Leaving it empty falls
# back to /proc/self/cgroup parsing + os.Hostname(). Typical compose
# snippet:
#
#   environment:
#     SWIRL_CONTAINER_ID: "${HOSTNAME}"
ENV SWIRL_CONTAINER_ID=""
EXPOSE 8001
ENTRYPOINT ["/app/swirl"]