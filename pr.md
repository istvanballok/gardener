An environment to try out Gardener in a safe way

**How to categorize this PR?**
/area dev-productivity
/kind enhancement

**What this PR does / why we need it**:

This PR allows to deploy the local setup of Gardener to a pod in a Kubernetes
cluster. It is similar to the remote local setup, and in addition, it provides a
locked down "jail" environment.

A web application that is not part of this PR could open a web terminal that
exec-s right into the jail to allow untrusted users to interact with the local
setup of Gardener.

The jail contains useful tools like `kubectl`, `gardenctl`, `kubens`, `kubectx`,
`jq`, ... to interact with Gardener. The untrusted user enters a tmux session
that is prepared with useful commands to create a shoot cluster, upgrade it to a
managed seed cluster, or just list the pods on the various levels of the
Gardener hierarchy (Garden, Seed, Shoot).

The jail is locked down: the CPU and memory usage is limited, the file system is
backed by a small tempfs, there is no Internet access: only the Gardener
endpoints are accessible.

To prevent breaking out of this jail via a Gardener shoot, a modified version of
Gardener is used such that the admin kubeconfig of the shoot will not allow the
untrusted user to start, exec into or attach to executable artifacts. So the
user is able to create a shoot, list the pods of the shoot and the control
plane, but it can't run or exec into those pods.

To prevent breaking out of this jail via the KinD cluster, a kubeconfig with a
service account is injected into the jail, that allows to create shoots, list
pods, but does not allow to exec into or attach to pods.

Nvim is provided to allow browsing the Gardener source code or to edit the
example artifacts like the shoot and managed seed yamls in a colorful way.

If the untrusted user creates a port-forward to a component like Plutono, the
web application that is managing these environments could establish (even
beforehand) an ingress to some defined port like 8080, 3000, ..., such that a
link can be opened in the Browser that will be forwarded to this deeply nested
environment.

The endpoints of the local setup could be also exposed publicly, so that the
user can interact with the Gardener API directly on his laptop, but accessing it
via the jailed tmux session in the web terminal that supports true color will
probably be more convenient.

A key logger is tracking all the activity inside the jail for auditing purposes.
The input is limited to 1MB of characters, to avoid typing in a huge file.

**Which issue(s) this PR fixes**:
Fixes #

**Special notes for your reviewer**:
/cc @vicwicker @rfranzke

**Release note**:
```other developer
Add an environment to try out Gardener in a safe way
```
