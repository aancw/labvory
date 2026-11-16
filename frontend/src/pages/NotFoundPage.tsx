import { Link } from "react-router-dom"
import { SearchXIcon } from "lucide-react"

import { EmptyState } from "@/components/EmptyState"
import { Button } from "@/components/ui/button"

export function NotFoundPage() {
  return (
    <div className="mx-auto max-w-lg pt-12">
      <EmptyState
        icon={SearchXIcon}
        title="Page not found"
        description="The page you requested does not exist in this instance."
      />
      <div className="mt-4 text-center">
        <Button asChild variant="outline" size="sm">
          <Link to="/">Back to dashboard</Link>
        </Button>
      </div>
    </div>
  )
}
