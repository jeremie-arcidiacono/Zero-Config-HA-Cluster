#import "lib/lib.typ" as hepia
#import "@preview/acrostiche:0.7.0": *
#import "globals.typ": urls

#show: hepia.bachelor.with(
  title: [Zero config high-availability cluster],
  short-title: title => [Zero config high-availability cluster], // Vous pouvez utiliser une fonction dans ce format pour créer votre titre court (en-tête de page), sinon le titre est utilisé
  author: (
    firstname: [Jérémie],
    lastname: [Arcidiacono]
  ),
  date: datetime(day: 19, month: 08, year: 2026), // Saisir la date du dépôt
  teachers: (
    [Florent Glück],
  ),
  clients: (
    [ANTS A.I. Systems],
  ),
  illustration: (
    illustration: image("assets/images/main-project-illustration.jpg"),
    legend-source: [
      Ensemble de serveurs interconnectés représentant un système distribué.
      Source : #hepia.source_url(urls, 0)
    ],
  ),
  abstract: [
    Le déploiement d'une infrastructure d'intelligence artificielle requiert généralement des connaissances techniques poussées et une gestion complexe. Ce Travail de Bachelor, réalisé avec l'entreprise ANTS A.I. Systems à la suite du projet de semestre qui en a posé les bases, vise à implémenter un système distribué « zéro-configuration » et à haute disponibilité. Le principe est simple : un client sans compétence technique achète les machines, les branche, et le cluster se forme et se maintient de lui-même, sans intervention de sa part. La solution repose sur deux couches : une couche basse, fondée sur l'outil Serf, chargée de la découverte des nœuds et de la détection des pannes, et une couche haute confiée à K3s, une distribution légère de Kubernetes, pour l'orchestration des applications. Le pont entre les deux est assuré par antsd, un daemon écrit en Go, développé durant ce travail, qui s'exécute sur chaque machine et pilote l'installation autonome de K3s. Une image système dédiée, ants-os, a également été construite pour l'architecture ARM64, air-gapped et prête à être flashée sur un nœud. Le comportement du système a été validé sur un banc d'essai de six Raspberry Pi 5, à travers une campagne de tests reproduisant la formation du cluster, l'ajout de machines, les redémarrages et diverses pannes injectées. Les résultats montrent qu'un cluster encaisse sans intervention manuelle le retrait ou le redémarrage de machines, y compris une coupure générale, et que l'ajout de nouveaux nœuds se déroule de manière tout aussi automatique et décentralisée. Une investigation sur la sécurisation de l'architecture a par ailleurs abouti à des recommandations priorisées, dont la fermeture de l'adhésion au cluster comme mesure centrale.
  ],
  abstract-illustration: image(
    "assets/images/main-project-illustration.jpg",
    alt: "Ensemble de serveurs interconnectés représentant un système distribué",
    height: 25%
    ),
  topic: include "contents/topic_content.typ",
  internship: false,
  confidential: false,
  orientation: "logiciel", //Compléter avec "sécurité" ou "logiciel" ou "embarqué"
  dedication: none,
  acknowledgement: [
    Je souhaite remercier M. Florent Glück pour son encadrement et ses conseils tout au long de ce projet, à la fois lors du projet de semestre et du Travail de Bachelor.
    Sa disponibilité et sa réactivité ont été précieuses.
    Je remercie également M. Guillaume Chanel pour la création de la feuille de style Typst utilisée pour l'écriture de ce rapport.
    ],
  acronyms: (
    "HEPIA": ("Haute école du paysage, d'ingénierie et d'architecture"),
    "HES-SO": ("Haute école spécialisée de Suisse Occidentale"),
    "PoC": ("Proof of Concept"),
    "SWIM": ("Scalable Weakly Consistent Infection-style Process Group Membership"),
    "DHCP": ("Dynamic Host Configuration Protocol"),
    "NTP": ("Network Time Protocol"),
    "VLAN": ("Virtual Local Area Network"),
    "CIS": ("Center for Internet Security"),
    "TPM": ("Trusted Platform Module"),
    "mDNS": ("Multicast DNS"),
    "RPC": ("Remote Procedure Call"),
    "GPL": ("GNU General Public License"),
    "HA": ("High Availability"),
  ),
  figures_urls: urls,
  introduction: include("contents/introduction.typ"),
  conclusion: include("contents/conclusion.typ"),
  appendixes: (
    include "contents/annexe_antsd-states.typ",
    include "contents/annexe_testbed-inventory.typ",
  ),
  bibliography-bytes: read("bibliography.bib", encoding: none)
)

#include("contents/1_context.typ")
#include("contents/2_conception.typ")
#include("contents/3_implementation.typ")
#include("contents/4_tests.typ")
#include("contents/5_security.typ")
#include("contents/6_results.typ")