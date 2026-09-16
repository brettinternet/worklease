# Remote invite artifacts

`worklease server init --guided` and `worklease invite issue` create a compact,
owner-private, single-line invite artifact. Transfer that whole file through an
authentic confidential channel (for example, a password manager or an
authenticated encrypted copy). The token encoding is bounded and versioned,
but it is not a signature: substituting the whole artifact cannot be detected
on a fresh client unless the transfer channel authenticates it. An established
profile rejects endpoint, authority-ID, or certificate-pin collisions.

Enroll with `worklease enroll --invite-file invite.artifact`. The artifact's
profile hint supplies the profile name when `--profile` is omitted. An explicit
`--profile` takes precedence. An HTTP artifact additionally requires
`--allow-insecure-http`; the artifact itself never enables cleartext transport.
Pinned HTTPS artifacts validate the exact DER leaf certificate, endpoint
hostname/IP, and certificate validity for every request. Redirects are never
followed.

Restore and bootstrap reissue continue to accept and emit legacy bare invite
secrets. Recovery operators should transfer the authority endpoint and ID from
an authenticated recovery record, then establish a profile with
`worklease profile add NAME --endpoint ... --authority-id ...
--certificate-sha256 ...` before redeeming a legacy secret. The certificate
pin is required for a self-signed TLS authority and omitted for a certificate
that normal CA verification already trusts. Restore-incarnation checks remain
mandatory; do not bypass them
by copying a profile from an untrusted source.
