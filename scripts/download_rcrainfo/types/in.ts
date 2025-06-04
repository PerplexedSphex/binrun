export interface DownloadRcrainfoInput {
  /**
   * Which dataset to run the ETL for.
   */
  dataset: "rcra" | "ce" | "ca" | "br" | "emanifest";
  /**
   * Skip downloading data, only run the loading into DuckDB.
   */
  skip_download?: boolean;
}
