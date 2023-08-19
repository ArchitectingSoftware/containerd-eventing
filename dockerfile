# syntax=docker/dockerfile:1

FROM golang:1.20 AS build-stage

# Set destination for COPY
WORKDIR /app

#cache the dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy files
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /containerd-events


FROM alpine:latest AS run-stage

# JUST put in root
WORKDIR /

# Copy binary from build stage
COPY --from=build-stage /containerd-events /containerd-events

# Expose port
EXPOSE 9980

# Run
CMD ["/containerd-events"]