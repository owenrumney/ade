// What the launcher answers a review request with.
export interface Asked {
  to: string;
  reviewerSet?: boolean;
  reviewer?: string;
  reviewerNote?: string;
}
