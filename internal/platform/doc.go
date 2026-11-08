// Package platform is RKL's OS integration behind interfaces.
//
// Rules:
//   - Exactly one global input hook per process. Hook callbacks only hand events off with
//     [TrySend].
//   - Missing permissions are reported through [Capabilities], never by panicking or spinning.
//   - Event streams ([WindowTracker.Subscribe], [InputListener.Start], [SignalTriggers]) run
//     until their context is done, then release their OS resources and close the channel.
//   - Backends live in sub-packages; [Detect] chooses one at runtime.
package platform
