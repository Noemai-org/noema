# deploy/relay-host

Date: 2026-11-06. Task ENV-13 (REQ-API-008; DEC-2026-10-16 E5, E6). SYNTHETIC test environment, not a Noema deployable (AGENT-25).

Puts the webhook relay (`synthetic/webhook_relay`) on one small public host so Tushe can deliver webhooks to it. The host runs nothing else. `setup.sh` points a DuckDNS name at the host, starts the relay on loopback and starts Caddy in front of it (decision A11: HTTP-01 on ports 80 and 443, no DNS plugin). The DuckDNS token never reaches Caddy.

## Founder click-script

You need: a DuckDNS account, an AWS account, about 20 minutes. The relay's address is a DuckDNS name the Founder reserves in step 1. By DEC-2026-11-05 D5 it is not written in any repository file: it lives in the GitHub Environment variable `RELAY_HOSTNAME` (and in the Founder's issue). Below, `<NAME>` stands for the subdomain and `$RELAY_HOSTNAME` for the full name; nothing in the script depends on which name it is.

1. **DuckDNS name.** Sign in at duckdns.org. Under "domains", add the subdomain `<NAME>` (the full name is `<NAME>.duckdns.org`). Copy your token from the top of the page (it is a single token of letters, digits and hyphens; the script refuses a token file with anything else in it, or more than one line). Do not type an IP address by hand and do not use your own computer's address: the record must point at the instance's static IP (step 3), which the script sets for you in step 7 (it sends no address, so DuckDNS uses the one the instance calls from).
2. **Lightsail instance.** In the AWS console open Lightsail, then "Create instance". Platform Linux/Unix, blueprint "OS only", Ubuntu 24.04 LTS, the smallest plan. Name it `noema-relay`. Create.
3. **Static IP.** Lightsail, Networking, "Create static IP". Attach it to `noema-relay`. Do this before step 7: the DuckDNS record must point at the static address, which survives a restart.
4. **Ports 80 and 443.** Open the instance, Networking tab, "IPv4 Firewall", "Add rule" twice: HTTP (TCP 80) and HTTPS (TCP 443), both from any IPv4 address. Port 80 must stay open: Caddy renews its certificate over it. SSH (22): edit the existing rule and tick "Restrict to IP address", then enter your own current public address (the one you will connect from); leave no other rule open. Do not open 8090: the relay listens on 127.0.0.1 only.
5. **Install Docker.** On the instance page click "Connect using SSH", then run:

   ```sh
   sudo apt-get update && sudo apt-get install -y docker.io docker-compose-v2 git curl
   sudo usermod -aG docker "$USER"
   ```

   Close the SSH window and open it again so the group change applies.
6. **Token file and code.** In the new SSH window:

   ```sh
   install -m 600 /dev/null ~/duckdns.token && nano ~/duckdns.token   # paste the token, save, exit
   git clone https://github.com/Noemai-org/noema.git && cd noema
   ```

   (Until noema#189 merges, also run `git checkout feat/ENV-13-relay-host-script`.)
7. **Run the script.** First the dry run, which changes nothing; then the real one:

   ```sh
   export RELAY_HOSTNAME=<NAME>.duckdns.org DUCKDNS_TOKEN_FILE="$HOME/duckdns.token"
   deploy/relay-host/setup.sh --check
   deploy/relay-host/setup.sh
   ```

   It prints "the relay is reachable by name" and exits 0 when it works. It exits 3 and says what to check if the name does not answer within a minute.
8. **Test the URL.** From any computer: `curl -i https://$RELAY_HOSTNAME/healthz` must answer `200`. A browser showing a valid padlock on that address confirms the certificate. Also check the redirect (A11) and the closed routes: `curl -sI http://$RELAY_HOSTNAME/healthz` must answer a redirect (308) to HTTPS, and `curl -s -o /dev/null -w '%{http_code}' https://$RELAY_HOSTNAME/v1/stats` must answer `404`.

When step 8 answers `200`, send Tushe the message in `tushe-address-message.md` (host, TLS, health path, what Tushe sends and what we need back). Do not send it before: Tushe asked for the address (deck v4, R13), and an address that does not answer is worse than none. The signing secret Tushe returns goes into the relay's volume file only (see below), never into the core's environment.

## What the script does not do

- It does not handle the signing secret. The relay's one-shot writes a fresh one into a Docker volume; to use the secret Tushe was given, write it to that volume file (see `deploy/compose/services/webhook-relay.yml`).
- The public name forwards only `/webhooks` (Standard Webhooks signature) and `/healthz`; every other path, including the relay's token-protected `/v1/deliveries` and `/v1/stats`, is a 404 there. The core pulls inside `tushe_net`. Caddy's admin API is off. The container drops every capability but `NET_BIND_SERVICE`, sets no-new-privileges and has a read-only root filesystem; it still runs as root inside the container, because a non-root process cannot gain the port-binding capability under no-new-privileges on the host network.
- Not exercised yet: the DuckDNS call, the Docker start and Caddy's certificate request (no Docker daemon or token where this was written). The first run on the host is the test; report its output.
