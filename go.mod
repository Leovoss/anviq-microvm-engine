module github.com/Leovoss/anviq-microvm-engine

go 1.22

require github.com/firecracker-microvm/firecracker-go-sdk v1.0.0

// Run `go mod tidy` on a host with network access to populate go.sum and the
// firecracker-go-sdk transitive dependencies. Pinned to a released tag on purpose.
