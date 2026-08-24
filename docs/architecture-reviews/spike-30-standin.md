# Stand-in for #30

This file is not the spike's findings. #30 ("Spike: the retarget and Mergify's queue") is real,
unfinished human work: two hand-made PRs on `support-app`, observed by hand, written up as an
answer once run. Nobody has done that yet.

This PR exists only so command-centre's dependency graph has something to point at. #38 is
blocked by #30, and the app's unlock check has exactly one vocabulary: does the blocker's branch
carry an open or merged PR. A hand-done spike with no lasting branch of its own can never satisfy
that check, so #38 could never unlock through the app at all without this.

Delete this file and replace it with the real write-up once #30 is actually run.
