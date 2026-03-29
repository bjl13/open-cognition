FROM python:3.12-slim AS builder

WORKDIR /build

# Install build deps
RUN pip install --no-cache-dir hatchling

COPY pyproject.toml ./
COPY observer/ ./observer/

RUN pip install --no-cache-dir --prefix=/install .

# ---------------------------------------------------------------------------

FROM python:3.12-slim

WORKDIR /app

# Copy installed packages
COPY --from=builder /install /usr/local

COPY observer/ ./observer/

# Non-root runtime user
RUN useradd --no-create-home --shell /sbin/nologin observer
USER observer

# Required
ENV CONTROL_PLANE=http://control-plane:8080
ENV AGENT_ID=observer:default

# Optional — leave unset to collect environment snapshot
# ENV OBSERVE_TARGET=

# Interval between observation cycles (seconds)
ENV POLL_INTERVAL=60

CMD ["python", "-m", "observer.agent"]
