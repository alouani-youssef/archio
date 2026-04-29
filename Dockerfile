# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
# CGO_ENABLED=0 is used to ensure a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o archio-api ./cmd/api/main.go

# Final stage
FROM alpine:latest

# Set working directory for the final container
WORKDIR /app

# Copy the pre-built binary file from the previous stage
COPY --from=builder /app/archio-api .

# Expose the application port
EXPOSE 3000

# Command to run the executable
CMD ["./archio-api"]
