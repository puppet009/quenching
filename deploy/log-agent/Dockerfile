# Fixed, standard log collector; build validates VRL/config before image import.
FROM timberio/vector:0.59.0-alpine
COPY deploy/helm/files/log-agent.yaml /etc/vector/vector.yaml
RUN VECTOR_DANGEROUSLY_ALLOW_ENV_VAR_INTERPOLATION=true VECTOR_SELF_NODE_NAME=build POD_NAMESPACE=platform \
    vector validate --no-environment --skip-healthchecks /etc/vector/vector.yaml && \
    VECTOR_DANGEROUSLY_ALLOW_ENV_VAR_INTERPOLATION=true VECTOR_SELF_NODE_NAME=build POD_NAMESPACE=platform \
    vector test /etc/vector/vector.yaml
