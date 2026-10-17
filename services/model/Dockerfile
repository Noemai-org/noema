# Build context: services/model/ only (DEC-2026-10-13 R2). Build with: docker build services/model
FROM python:3.13.16-slim-bookworm@sha256:a1165e272e578941b84abc79e4ab38a0305cd12803a5c4247979ac7655f4d641
LABEL noema.service="model"
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY requirements.lock ./
RUN pip install --no-cache-dir --require-hashes -r requirements.lock
COPY noema_model ./noema_model
RUN useradd --system --uid 10001 noema
USER noema
EXPOSE 8081
ENTRYPOINT ["python", "-m", "uvicorn", "noema_model.main:app", "--host", "0.0.0.0", "--port", "8081"]
