// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package lqo is LQO, a learned query optimizer for PostgreSQL. It finds the
// queries the PostgreSQL planner gets wrong and the planner settings that
// fix them, by running each query shape under the default plan and under a
// set of hints, against a replica or a copy, and keeping a hint only when it
// is clearly and consistently faster.
//
// lqo advise reads a workload, runs the trials and reports. lqo serve runs as
// a proxy in front of the server, measures the live queries and learns on a
// replica which hints help; in active mode, since 0.3, it also applies the
// proven hints to live queries and takes back any that run slower. The code
// is in the internal packages; the command is in cmd/lqo.
package lqo

// Version is the LQO release. The release workflow reads it from this line.
const Version = "0.3.0"
