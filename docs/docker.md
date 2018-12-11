# Docker Support

The inclusion of a Dockerfile in the dcrdata repository means you can use Docker
for dcrdata development or in production. However, official images are not
presently published to docker hub.

When developing you can utilize containers for easily swapping out Go versions
and overall project setup. You don't even need go installed on your system if
using containers during development.

Once [Docker](https://docs.docker.com/install/) is installed, you can then
download this repository and follow the build instructions below.

## Building the Image

To use a dcrdata container you need to build an image as follows:

`docker build --squash -t decred/dcrdata:dev-alpine .`

Note: The `--squash` flag is an [experimental
feature](https://docs.docker.com/engine/reference/commandline/image_build/) as
of Docker 18.06. Experimental features must be enabled to use the setting. On
Windows and OS/X, look under the "Daemon" settings tab. On Linux, [enable the
setting manually](https://github.com/docker/cli/blob/master/experimental/README.md).

By default, docker will build the container based on the Dockerfile found in the
root of the repository that is based on Alpine Linux. To use an Ubuntu-based
container, you should build from the Ubuntu-based Dockerfile:

`docker build --squash -f dockerfiles/Dockerfile_stretch -t decred/dcrdata:dev-stretch .`

Part of the build process is to copy all the source code over to the image,
download all dependencies, and build dcrdata. If you run into build errors with
docker try adding the `--no-cache` flag to trigger a rebuild of all the layers
since docker does not rebuild cached layers.

`docker build --no-cache --squash -t decred/dcrdata:dev-alpine .`

## Building dcrdata with Docker

In addition to running dcrdata in a container, you can also build dcrdata inside
a container and copy the executable to another system. To do this, you must have
the dcrdata Docker image or [build it from source](#building-the-image).

The default container image is based on amd64 Alpine Linux. To create a binary
targeting different operating systems or architectures, it is necessary to [set
the `GOOS` and `GOARCH` environment variables](https://golang.org/doc/install/source#environment).

From the repository source folder, do the following to build the Docker image,
and compile dcrdata into your current directory:

- `docker build --squash -t decred/dcrdata:dev-alpine .` [Only build the container image if necessary](#building-the-image)
- `docker run --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine go build`

This mounts your current working directory in the host machine on a volume
inside the container so that the build output will be on the host file system.

Build for other platforms as follows:

`docker run -e GOOS=darwin -e GOARCH=amd64 --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine go build`

`docker run -e GOOS=windows -e GOARCH=amd64 --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine go build`

## Developing dcrdata Using a Container

Containers are a great way to develop any source code as they serve as a
disposable runtime environment built specifically to the specifications of the
application. Suggestions for developing in a container:

1. Don't write code inside the container.
2. Attach a volume and write code from your editor on your docker host.
3. Attached volumes on a Mac are generally slower than Linux/Windows.
4. Install everything in the container, don't muck up your Docker host.
5. Resist the urge to run git commands from the container.
6. You can swap out the Go version just by using a different docker image.

To make the source code from the host available inside the container, attach a
volume to the container when launching the image:

`docker run -ti --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine /bin/bash`

_Note_: Changing `entrypoint` allows you to run commands in the container since
the default container command runs dcrdata. We also added /bin/bash at the
end so the container executes this by default.

You can now run `go build` or `go test` inside the container. If you run `go fmt`
you should notice that any formatting changes will also be reflected on the
docker host as well.

To run dcrdata in the container, it may be convenient to use [environment
variables](#using-configuration-environment-variables) to configure dcrdata. The
variables may be set inside the container or on the [command
line](https://docs.docker.com/engine/reference/run/#env-environment-variables).
For example,

`docker run -ti --entrypoint=/bin/bash -e DCRDATA_LISTEN_URL=0.0.0.0:2222 -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine`

## Container Production Usage

We don't yet have a build system in place for creating production grade images
of dcrdata. However, you can still use the images for testing.

In addition to configuring dcrdata, it is also necessary to map the TCP port on
which dcrdata listens for connections with the `-p` switch. For example,

`docker run -ti -p 2222:2222 -e DCRDATA_LISTEN_URL=0.0.0.0:2222 --rm decred/dcrdata:dev-alpine`

Please keep in mind these images have not been hardened so this is not
recommended for production.

Note: The TLS certificate for dcrd's RPC server may be needed in the container.
Either build a new container image with the certificate, or attach a volume
containing the certificate to the container.
