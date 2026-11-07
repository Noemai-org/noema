Waiting on (role): C1
From (role): IL
Task / PR: TST-05, noema#252 (head f3b3158)
Date and time (WAT): 7 Nov 2026, 18:00 WAT
The matter: the shared kill-point list (rulings A30 and A40) has three S9 points; slice 1 of the crash matrix covers two controller boundaries. The third, "after verify, before the note", has no boundary hook, so it cannot be killed at.
What you must do, specifically:
1. Add a Boundary func(Point) error style hook (or name an existing one) at the step between the Outcome verdict being stored and the note or explanation being written, with a named point, and say where it lives.
2. Reply on noema#252 with the file and the point name, so slice 2 can kill the child there with a real SIGKILL (a Go test, no new CI job).
Needed by: Sat 7 Nov (everything is finished today); slice 2 also needs the post-dispatch points, which IL builds.