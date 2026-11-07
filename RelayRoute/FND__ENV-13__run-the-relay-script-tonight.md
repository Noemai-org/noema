Waiting on (role): FND
From (role): IL
Task / PR: ENV-13, noema#189 (runbook is in the PR body; it needs no merge)
Date and time (WAT): 7 Nov 2026, 18:00 WAT
The matter: the relay host script and Caddy have never run against DuckDNS, Docker or Caddy. The first run on a real host is the test. Tushe also asked for our HTTPS URL (R13).
What you must do, specifically:
1. Follow the Runbook section in the body of noema#189, steps A to D (about 20 minutes): reserve noema-relay-4821 at DuckDNS, create the Lightsail instance with a static IP and ports 80 and 443 open (SSH restricted to your address), put the token in a mode-600 file on the instance, then run deploy/relay-host/setup.sh --check and deploy/relay-host/setup.sh from the branch feat/ENV-13-relay-host-script.
2. Send back, without any secret: the full output of the --check and the real run, and the output of curl -i https://noema-relay-4821.duckdns.org/healthz.
3. Only after /healthz answers 200, send Tushe the message in deploy/relay-host/tushe-address-message.md.
Needed by: tonight (Sat 7 Nov).