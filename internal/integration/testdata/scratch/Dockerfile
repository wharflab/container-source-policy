FROM golang:1.21 AS builder
WORKDIR /app
RUN go build -o main .

FROM scratch
COPY --from=builder /app/main /main
CMD ["/main"]
