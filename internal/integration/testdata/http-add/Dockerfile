# Test ADD directive with HTTP URL (without checksum)
# This should be subject to source policy pinning
FROM alpine:3.18

# ADD with GitHub release URL - no checksum specified
# Uses a versioned release that has stable digest via GitHub API
ADD https://github.com/cli/cli/releases/download/v2.85.0/gh_2.85.0_checksums.txt /checksums.txt

RUN cat /checksums.txt
