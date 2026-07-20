TYPST_DOCKER_IMAGE = tb/typst:local
TYPST_DOCKER_IMAGE_DIR = docs/docker/typst

MERMAID_DOCKER_IMAGE = tb/mermaid:local
MERMAID_DOCKER_IMAGE_DIR = docs/docker/mermaid

REPORT_DIR = docs/report
REPORT_MAIN_FILE = main.typ
REPORT_OUTPUT_FILE = report.pdf

DIAGRAMS_DIR = $(REPORT_DIR)/assets

# Run as the caller: the image's own user (uid 1001) cannot write to the mount.
DOCKER_USER ?= $(shell id -u):$(shell id -g)

DOCKER_RUN_ARGS = --rm \
	-e XDG_CACHE_HOME=/typst-cache \
	-e XDG_DATA_HOME=/typst-data \
	-v typst-pkg-cache:/typst-cache \
	-v typst-pkg-data:/typst-data

.PHONY: generate-report help build-docker-typst build-docker-mermaid generate-diagrams

help:
	@echo "Commandes disponibles :"
	@echo "  build-docker-typst    - Construit l'image Docker Typst pour la génération du rapport"
	@echo "  generate-report       - Compile le rapport en PDF"
	@echo "  build-docker-mermaid  - Construit l'image Docker Mermaid pour la génération des diagrammes"
	@echo "  generate-diagrams     - Compile les diagrammes Mermaid (.mmd) en SVG"

build-docker-typst:
	docker build -t $(TYPST_DOCKER_IMAGE) $(TYPST_DOCKER_IMAGE_DIR)

generate-report: generate-diagrams
	docker run $(DOCKER_RUN_ARGS) -v $(PWD)/$(REPORT_DIR):/src $(TYPST_DOCKER_IMAGE) compile --root /src /src/$(REPORT_MAIN_FILE) /src/$(REPORT_OUTPUT_FILE)

build-docker-mermaid:
	docker build -t $(MERMAID_DOCKER_IMAGE) $(MERMAID_DOCKER_IMAGE_DIR)

generate-diagrams:
	docker run --rm -u $(DOCKER_USER) -e FORCE=$(FORCE) -v $(PWD)/$(DIAGRAMS_DIR):/data $(MERMAID_DOCKER_IMAGE)
