// Package readhostility holds the tests of the Evidence read path under the hostility proxy's faults (EVD-05, the hostility-proxy part, A717; REQ-CTX-008,
// REQ-TST-004, REQ-CTX-002; INV-07, INV-19, INV-23; AGENT-12). It has no production code.
//
// The exchanges in testdata/read_hostility_recordings.json were made by the real hostility proxy (benchmark/hostility_proxy, TestReadHostilityExchanges), which
// regenerates the file and fails if the committed copy differs. The tests replay them to the real StandinClient and the real portread.Reader and require the
// status that layer.AssignStatus gives each item. Nothing here mocks a failure. All data is SYNTHETIC (AGENT-25).
package readhostility
