Support a remote environment to try out Gardener in a safe way

**How to categorize this PR?**
/area dev-productivity
/kind enhancement

**What this PR does / why we need it**:

This PR allows to deploy the [local setup][] of Gardener to a pod in a
Kubernetes cluster: the `gardener-operator` based local setup is a complete
landscape that contains all the layers of Gardener.

[local setup]: https://github.com/gardener/gardener/blob/master/docs/deployment/getting_started_locally.md#alternative-way-to-set-up-garden-and-seed-leveraging-gardener-operator

This setup is based on the "[remote local setup][]" and it provides a
locked-down, "jail" environment in addition where _untrusted_ users could be
allowed to interact with Gardener in a restricted and safe way.

[remote local setup]: https://github.com/gardener/gardener/blob/master/docs/deployment/getting_started_locally.md#remote-local-setup

A web application (that is not part of this PR) could show a web terminal that
exec-s right into the locked-down, "jail" environment to allow untrusted users
to interact with Gardener in a Browser session, without the need to install or
configure anything. Note that to try out Gardener without the restrictions of
this jail environment, untrusted users are welcome to run the "local setup"
directly on their laptops or deploy the "remote local setup" to a Kubernetes
cluster of their choice.

Having a "jail" environment where even untrusted users could be allowed to try
out Gardener even in a browser session, without the need to install or configure
anything, could be useful for KubeCon conference booths, to allow
some hands on experience with Gardener without any hurdles.

Although the jail environment is restricted, it is well equipped to make the
first experience of the untrusted user as pleasant as possible. Useful tools
like `kubectl`, `gardenctl`, `jq`, ... allow to interact with Gardener
efficiently in the command line. A `tmux` session is prepared with commands to
walk through the main features of Gardener: create a shoot cluster, access the
shoot cluster and also its control plane in the seed. Even the scenario of
upgrading a vanilla shoot cluster to a managed seed is supported. The untrusted
user can use tools like `k9s` in vivid colors and with mouse support to inspect
that state of Gardener during its lifecycle.

The jail environment is locked down: the CPU and memory usage is limited, the
file system of the container is backed by a small tempfs to avoid writing to the
root disk of the node. Creating PID or file descriptors is restricted to prevent
abuse. There is no Internet access but the Gardener endpoints are accessible.

To prevent breaking out of the jail via the KinD cluster that hosts the local
setup, a kubeconfig is provided that does not allow to create create or
attach/exec into executable artifacts like pods. The permissions allow to create
shoots, list pods and configmaps. Secrets are not allowed to be listed, to avoid
gaining access to the shoot cluster.

To prevent breaking out of the jail via a Gardener shoot, a slightly modified
version of Gardener should be used such that the admin kubeconfig of the shoot
should not be an admin kubeconfig: it should not be possible for the untrusted
user to create, exec into or attach to pods in the shoot cluster. The
permissions allow to inspect the components of the shoot cluster.

Nvim is configured to allow browsing the Gardener source code or to edit the
example artifacts like the shoot and managed seed resources efficiently and in a
colorful way. The [OpenVSCode Server][] is also available to open the source
code in a Browser.

[OpenVSCode Server]: https://github.com/gitpod-io/openvscode-server

The web application that is managing these demo environments could prepare up
front a public ingress to a list of well defined ports like 3000, 9090, ... to
the remote local setup pod. If the untrusted user creates a port-forward to one
of these ports, indirectly that port will be exposed publicly such that a the
Prometheus or Plutono dashboards of this deeply nested and locked down
environment can be even opened in the Browser.

The Kubernetes endpoints of the local setup could be also exposed via an
ingress, so that the untrusted user could also interact with the Gardener API
directly on his laptop. Although accessing Gardener in the jailed tmux session
in the web terminal in true color and with mouse support will probably be more
convenient.

All the activity inside the jail environment is captured with the `script`
utility for auditing purposes. A hint is shown that untrusted users should not
enter sensitive information in the jail environment. The total input and output
is limited to 1 MB of characters that should be sufficient to interactively
explore the demo environment but it should help to avoid typing in malicious
executable files in hexadecimal format.

The interaction with the jail environment is logged using the `script` utility:

TODO: the idea of combine `script` with `pv -q -L 1` to apply rate limiting to
the input and maybe the output of `script` is not straightforward.

```bash
script --log-timing timing.log --log-out out.log --log-in input.log --output-limit 1MB

# To replay the session:
scriptreplay --log-timing timing.log --log-out out.log
```

Note that trusted users can also access the remote local setup directly, to see
what is happening inside and outside the jail.

**Which issue(s) this PR fixes**:
Fixes #

**Special notes for your reviewer**:
/cc @vicwicker @rfranzke

**Release note**:

```other developer
Support a remote environment to try out Gardener in a safe way
```

TODO: a deployment and roles to exec into the jail environment so that the web terminal can attach to that pod.

The logs should be written to the persistent volume of the remote local setup
pod. So the exec command could call script, and then exec into the containers.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: alpine-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: alpine
  template:
    metadata:
      labels:
        app: alpine
    spec:
      containers:
      - name: alpine
        image: alpine
        command:
        - /bin/sh
        - -c
        - |
          tee input.log | sh -i | tee output.log
        stdin: true
        tty: true
```
