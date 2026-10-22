# Built with examples/cicd-agent as the context (make docker-build-cicd-agent).
FROM python:3.12-slim
WORKDIR /app

COPY pyproject.toml ./
COPY src/ src/
RUN pip install --no-cache-dir . && useradd -u 10001 -m agent

USER agent
EXPOSE 8090
ENTRYPOINT ["python", "-m", "cicd_agent"]
