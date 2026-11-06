# deploy/relay-host

Date: 2026-11-06. Task ENV-13 (REQ-API-008; DEC-2026-10-16 E5, E6). SYNTHETIC test environment, not a Noema deployable (AGENT-25).

Puts the webhook relay (`synthetic/webhook_relay`) on one small public host so Tushe can deliver webhooks to it. The host runs nothing else. `setup.sh` points a DuckDNS name at the host, starts the relay on loopback and starts Caddy in front of it (decision A11: HTTP-01 on ports 80 and 443, no DNS plugin). The DuckDNS token never reaches Caddy.

## Founder click-script

You need: a DuckDNS account, an AWS account, about 20 minutes. Replace `NAME` with the name you reserve.

1. **DuckDNS name.** Sign in at duckdns.org. Under "domains", add the subdomain `NAME` (the full name is `NAME.duckdns.org`). Copy your token from the top of the page. Leave the IP as it is; the script sets it.
2. **Lightsail instance.** In the AWS console open Lightsail, then "Create instance". Platform Linux/Unix, blueprint "OS only", Ubuntu 24.04 LTS, the smallest plan. Name it `noema-relay`. Create.
3. **Static IP.** Lightsail, Networking, "Create static IP". Attach it to `noema-relay`. Do this before step 7: the DuckDNS record must point at the static address, which survives a restart.
4. **Ports 80 and 443.** Open the instance, Networking tab, "IPv4 Firewall", "Add rule" twice: HTTP (TCP 80) and HTTPS (TCP 443), both from any IPv4 address. Port 80 must stay open: Caddy renews its certificate over it. Leave SSH (22) as it is.
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
   export RELAY_HOSTNAME=NAME.duckdns.org DUCKDNS_TOKEN_FILE="$HOME/duckdns.token"
   deploy/relay-host/setup.sh --check
   deploy/relay-host/setup.sh
   ```

   It prints "the relay is reachable by name" and exits 0 when it works. It exits 3 and says what to check if the name does not answer within a minute.
8. **Test the URL.** From any computer: `curl -i https://NAME.duckdns.org/healthz` must answer `200`. A browser showing a valid padlock on that address confirms the certificate.

Do not send Tushe the URL or a signing secret yet: the signing secret and the route table wait for Tushe's written response (due Wed 11 Nov). The receiving address will be `https://NAME.duckdns.org/webhooks`.

## What the script does not do

- It does not handle the signing secret. The relay's one-shot writes a fresh one into a Docker volume; to use the secret Tushe was given, write it to that volume file (see `deploy/compose/services/webhook-relay.yml`).
- It does not restrict which relay routes Caddy forwards. `/v1/deliveries` and `/v1/stats` are protected by the relay's pull token; `/webhooks` by the Standard Webhooks signature.
- Not exercised yet: the DuckDNS call, the Docker start and Caddy's certificate request (no Docker daemon or token where this was written). The first run on the host is the test; report its output.
