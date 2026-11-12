package runshim

// reexecEnv makes the test binary act as `onett run-shim`, so the detached-launch path can be
// tested without a built onett binary.
const reexecEnv = "ONETT_RUNSHIM_REEXEC"

// ReexecEnv is reexecEnv for TestMain, which is in package runshim_test.
const ReexecEnv = reexecEnv
