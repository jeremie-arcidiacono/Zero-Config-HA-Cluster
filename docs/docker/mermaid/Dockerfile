ARG MERMAID_VERSION=11.16.0
FROM ghcr.io/mermaid-js/mermaid-cli/mermaid-cli:${MERMAID_VERSION}

LABEL authors="jeremie"

COPY render.sh /usr/local/bin/render.sh

ENTRYPOINT ["sh", "/usr/local/bin/render.sh"]
CMD []
