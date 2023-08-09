FROM arm64v8/golang AS builder

# install ca-certificates to copy them into the container
RUN apk --no-cache add ca-certificates

# use a separate workspace to isolate the artifacts
WORKDIR "/workspace"

# copy the go modules and manifests to download the dependencies
COPY "go.mod" "go.mod"
COPY "go.sum" "go.sum"

# cache the dependencies before copying the other source files so that this layer won't be invalidated on code changes
RUN go mod download -x

# copy all other files into the image to work on them
COPY "." "./"

# build the statically linked binary from the go source files
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o anserem main.go


FROM scratch

# copy the raw binary into the new container
COPY --from=builder "/workspace/anserem" "/anserem"

# copy the users and groups for the nobody user and group
COPY --from=builder "/etc/passwd" "/etc/passwd"
COPY --from=builder "/etc/group" "/etc/group"

# we run with minimum permissions as the nobody user
USER nobody:nobody

# just execute the raw binary without any wrapper
ENTRYPOINT ["/anserem"]
