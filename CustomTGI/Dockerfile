FROM ghcr.io/huggingface/text-generation-inference:1.4.4
WORKDIR /usr/src

USER root
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends bash; \
    rm -rf /var/lib/apt/lists/*

ENV NUMBA_DISABLE_CACHE=1
ENV NUMBA_CACHE_DIR=/tmp/numba_cache

# Switch to root to change file permissions
USER root

# Create /data directory and assign ownership to UID 1000
RUN mkdir -p /data && chown -R 1000:1000 /data

# Switch back to UID 1000 (the default non-root user in TGI base image)
USER 1000
