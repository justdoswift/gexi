# gexi-relay

The server half of gexi's self-hosted remote access. A Mac running gexi keeps
one control connection open to this relay; when a phone or another Mac reaches
the relay's public port, the relay asks the Mac for a data connection and
forwards bytes both ways. No public address, port forward, or third-party
service is involved, and the relay has no destination to choose: a tunnel only
ever reaches the gexi that claimed it.

```
phone / another gexi ──▶ relay :47824 ──▶ (control + data) ──▶ gexi on the Mac
                          ▲
                          └── TLS, cert pinned by the Mac, on :47825
```

## Run it

```sh
mkdir -p data && sudo chown -R 65532:65532 data   # the image runs as nonroot
docker compose run --rm relay init
docker compose up -d
```

`init` writes `data/config.json`, generates a self-signed certificate for the
host link, and prints what to paste into gexi:

```
  Address      <server>:47825
  Tunnel       mac
  Token        <64 hex characters>
  Fingerprint  <sha256 of the certificate>
```

Then, on the Mac: **Settings → Remote → Self-hosted relay**, switch it on, and
fill in those four values plus the client address clients should use (for
example `https://relay.example.com`).

Clients connect to `http://<server>:47824` out of the box. That is plain HTTP;
put your own HTTPS proxy in front, or give the tunnel a certificate:

```json
{
  "hostListen": ":47825",
  "tunnels": [
    {
      "name": "mac",
      "token": "<token>",
      "listen": ":47824",
      "tlsCert": "/data/fullchain.pem",
      "tlsKey": "/data/privkey.pem"
    }
  ]
}
```

With a certificate, clients use `https://<server>:47824` or the domain on the
certificate, and the phone's browser is happy without any proxy.

## What the relay can and cannot do

- It forwards bytes to the gexi that claimed the tunnel. It cannot be pointed
  at another host, a URL, or a port of its choosing, so it is not an open
  proxy.
- The host link is TLS with a certificate the Mac pins by fingerprint. A
  different certificate is refused, which is what makes a self-signed one
  enough.
- The tunnel token is 32 random bytes, compared in constant time. A name that
  does not exist and a token that does not match get the same answer.
- Clients still need the Mac's pairing code and token; the relay never sees
  them, and a client that is not paired gets the same 401 it would on the LAN.
- The relay terminates TLS for clients when you give it a certificate, so it
  can see that traffic — it is your server. The host link is separate and stays
  encrypted end to end between the Mac and the relay.

## Operate it

```sh
docker compose logs -f relay      # tunnel online/offline lines, no tokens
docker compose restart relay
```

Rotating a token: edit `data/config.json`, restart the relay, and paste the new
token into gexi. Rotating the certificate changes the fingerprint, which gexi
must be told too.

The relay keeps no state beyond `data/config.json`, `data/relay.crt`, and
`data/relay.key`. Back those up if you want a restart to keep the same
fingerprint; otherwise the Mac refuses the new one until you paste it.
