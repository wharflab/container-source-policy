# container-source-policy

Generate a Docker BuildKit **source policy** file (`docker buildx build --source-policy-file …`) by parsing Dockerfiles and pinning `FROM` images to
immutable digests.

This PyPI package ships a small Python launcher plus a prebuilt `container-source-policy` binary for your platform.

## Install

Recommended (isolated):

```bash
pipx install container-source-policy
```

Or into the current environment:

```bash
python -m pip install container-source-policy
```

## Usage

```bash
container-source-policy pin --stdout Dockerfile > source-policy.json
docker buildx build --source-policy-file source-policy.json -t my-image:dev .
```

## More info

See the upstream repository for full documentation:
<https://github.com/tinovyatkin/container-source-policy>
