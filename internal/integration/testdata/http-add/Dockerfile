# Test ADD directive with HTTP URL (without checksum)
# This should be subject to source policy pinning
FROM alpine:3.18

# ADD with HTTP URL - no checksum specified, making it non-reproducible
# Source policy can add http.checksum attribute to pin it
ADD https://raw.githubusercontent.com/moby/moby/master/README.md /README.md

RUN cat /README.md
