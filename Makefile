TYPST_DOCKER_IMAGE = tb/typst:local

TYPST_DOCKER_IMAGE_DIR = docs/docker/typst

REPORT_DIR = docs/report
REPORT_MAIN_FILE = main.typ
REPORT_OUTPUT_FILE = report.pdf

DOCKER_RUN_ARGS = --rm \
	-e XDG_CACHE_HOME=/typst-cache \
	-e XDG_DATA_HOME=/typst-data \
	-v typst-pkg-cache:/typst-cache \
	-v typst-pkg-data:/typst-data

.PHONY: generate-report help build-docker-typst

help:
	@echo "Commandes disponibles :"
	@echo "  build-docker-typst - Construit l'image Docker Typst pour la génération du rapport"
	@echo "  generate-report    - Compile le rapport en PDF"

build-docker-typst:
	docker build -t $(TYPST_DOCKER_IMAGE) $(TYPST_DOCKER_IMAGE_DIR)

generate-report:
	docker run $(DOCKER_RUN_ARGS) -v $(PWD)/$(REPORT_DIR):/src $(TYPST_DOCKER_IMAGE) compile --root /src /src/$(REPORT_MAIN_FILE) /src/$(REPORT_OUTPUT_FILE)
