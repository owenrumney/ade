FROM alpine:3.22
RUN apk add --no-cache git github-cli openssh-client
COPY provisioner/entrypoint.sh /usr/local/bin/ade-provision
# chmod rather than COPY --chmod: that needs BuildKit, and a worker building
# this locally may be on the legacy builder.
RUN chmod 0755 /usr/local/bin/ade-provision
ENTRYPOINT ["/usr/local/bin/ade-provision"]
