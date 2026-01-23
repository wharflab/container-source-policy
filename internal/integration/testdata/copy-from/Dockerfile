# Test: COPY --from with external image (not a build stage)
FROM alpine:3.18 AS final
COPY --from=busybox:1.36 /bin/busybox /bin/busybox
