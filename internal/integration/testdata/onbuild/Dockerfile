# Test ONBUILD instructions with ADD and COPY --from
FROM alpine:3.18
ONBUILD COPY --from=nginx:1.25 /etc/nginx/nginx.conf /etc/nginx/
ONBUILD ADD https://raw.githubusercontent.com/moby/moby/v28.0.1/README.md /app/
