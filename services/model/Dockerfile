# Build context: services/model/ only (DEC-2026-10-13 R2). Build with: docker build services/model
FROM python:3.13.16-slim-bookworm@sha256:a1165e272e578941b84abc79e4ab38a0305cd12803a5c4247979ac7655f4d641
LABEL noema.service="model"
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
# mTLS on the listener (noema#555; PERD 36.1: mTLS between the services, never a shared bearer). uvicorn reads these as its --ssl-* options, so every way of starting it in this image
# (the default entrypoint and the bench overlay's) requires a client certificate signed by the CA: CERT_REQUIRED is 2. The files are file-mounted at run time and are never in the
# image; if they are missing uvicorn does not start, so the service fails closed rather than serving plain HTTP. The core's client certificate is the peer identity.
ENV UVICORN_SSL_CERTFILE=/run/tls/model.crt UVICORN_SSL_KEYFILE=/run/tls/model.key UVICORN_SSL_CA_CERTS=/run/tls/ca.crt UVICORN_SSL_CERT_REQS=2
WORKDIR /app
COPY requirements.lock ./
RUN pip install --no-cache-dir --require-hashes -r requirements.lock
COPY noema_model ./noema_model
RUN useradd --system --uid 10001 noema
USER noema
EXPOSE 8081
ENTRYPOINT ["python", "-m", "uvicorn", "noema_model.main:app", "--host", "0.0.0.0", "--port", "8081"]
