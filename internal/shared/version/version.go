package version

// Version is the semantic version of the server and agent binaries.
const Version = "1.0.0"

// Protocol is the agent/server API protocol version. The server rejects
// agents speaking a different major protocol version.
const Protocol = 1
