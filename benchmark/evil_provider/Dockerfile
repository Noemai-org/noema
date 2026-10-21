# Build context: benchmark/evil_provider/ only (DEC-2026-10-13 R2). Build with: docker build benchmark/evil_provider
# The evil provider is a test double for the `bench` profile (T4 of DEC-2026-10-20). No product image holds this code.
FROM python:3.13.16-slim-bookworm@sha256:a1165e272e578941b84abc79e4ab38a0305cd12803a5c4247979ac7655f4d641
LABEL noema.service="evil-provider" noema.profile="bench"
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY . ./evil_provider
RUN useradd --system --uid 10003 evil
USER evil
EXPOSE 8090
ENTRYPOINT ["python", "-m", "evil_provider"]
