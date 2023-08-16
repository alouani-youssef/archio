# Use an official Go runtime as the base image
FROM golang:1.16 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go project source code into the container
COPY . .

# Build the Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -o myapp

# Use a minimal base image for the runtime environment
FROM alpine:latest

# Set the working directory inside the container
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/achrio .

# Default Port of ArchIO
EXPOSE 60013

# Run the binary when the container starts
CMD ["./archio"]